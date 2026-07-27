package usagestats

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

const (
	pluginName         = "builtin-account-cost-statistics"
	storageFileName    = "usage-statistics.json"
	flushInterval      = 2 * time.Second
	maxModelNameLength = 256
)

var (
	configuredStoreMu sync.Mutex
	configuredStore   *Store
)

// Store persists bounded account and model aggregates and implements usage.Plugin.
type Store struct {
	storage   snapshotStorage
	enabled   atomic.Bool
	mu        sync.RWMutex
	state     persistentState
	flushMu   sync.Mutex
	dirty     chan struct{}
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

type snapshotStorage interface {
	Load(context.Context) ([]byte, error)
	Save(context.Context, []byte) error
	Close() error
}

func configureStore(store *Store) {
	configuredStoreMu.Lock()
	defer configuredStoreMu.Unlock()

	if configuredStore != nil {
		configuredStore.SetEnabled(false)
		configuredStore.Close()
		configuredStore = nil
	}
	coreusage.RegisterNamedPlugin(pluginName, store)
	configuredStore = store
}

// CloseIf closes store when it is still the configured process-wide instance.
func CloseIf(store *Store) {
	if store == nil {
		return
	}
	configuredStoreMu.Lock()
	if configuredStore == store {
		configuredStore = nil
	}
	configuredStoreMu.Unlock()
	store.SetEnabled(false)
	store.Close()
}

// NewStore creates a file-backed store for isolated tests and migration tools.
// The production server uses NewPostgresStore instead.
func NewStore(path string, enabled bool) (*Store, error) {
	return newStore(context.Background(), newFileSnapshotStorage(path), enabled)
}

func newStore(ctx context.Context, storage snapshotStorage, enabled bool) (*Store, error) {
	if storage == nil {
		return nil, fmt.Errorf("usage statistics: storage is required")
	}
	store := &Store{
		storage: storage,
		state:   newPersistentState(),
		dirty:   make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	store.enabled.Store(enabled)
	if errLoad := store.load(ctx); errLoad != nil {
		_ = storage.Close()
		return nil, errLoad
	}
	if store.insertMissingBuiltinPrices(time.Now().UnixMilli()) {
		if errSave := store.saveSnapshot(); errSave != nil {
			log.WithError(errSave).Warn("usage statistics: failed to persist built-in model prices")
		}
	}
	go store.run()
	return store, nil
}

func newPersistentState() persistentState {
	return persistentState{
		Version:    storageVersion,
		Prices:     make(map[string]ModelPrice),
		Aggregates: make(map[string]Aggregate),
	}
}

// SetEnabled toggles collection without affecting persisted data or prices.
func (s *Store) SetEnabled(enabled bool) {
	if s != nil {
		s.enabled.Store(enabled)
	}
}

// Enabled reports whether new usage records are collected.
func (s *Store) Enabled() bool {
	return s != nil && s.enabled.Load()
}

// HandleUsage aggregates a usage record without retaining raw credentials or request bodies.
func (s *Store) HandleUsage(_ context.Context, record coreusage.Record) {
	if s == nil || !s.enabled.Load() {
		return
	}
	aggregate, key := aggregateFromRecord(record)

	s.mu.Lock()
	current := s.state.Aggregates[key]
	if current.AccountKey == "" {
		current = aggregate
		current.Calls = 0
		current.SuccessCalls = 0
		current.FailureCalls = 0
		current.InputTokens = 0
		current.OutputTokens = 0
		current.ReasoningTokens = 0
		current.CachedTokens = 0
		current.CacheReadTokens = 0
		current.CacheWriteTokens = 0
		current.TotalTokens = 0
		current.LongInputTokens = 0
		current.LongOutputTokens = 0
		current.LongCachedTokens = 0
		current.LongCacheReadTokens = 0
		current.LongCacheWriteTokens = 0
	}
	current.Calls++
	if aggregate.FailureCalls > 0 {
		current.FailureCalls++
	} else {
		current.SuccessCalls++
	}
	current.InputTokens += aggregate.InputTokens
	current.OutputTokens += aggregate.OutputTokens
	current.ReasoningTokens += aggregate.ReasoningTokens
	current.CachedTokens += aggregate.CachedTokens
	current.CacheReadTokens += aggregate.CacheReadTokens
	current.CacheWriteTokens += aggregate.CacheWriteTokens
	current.TotalTokens += aggregate.TotalTokens
	current.LongInputTokens += aggregate.LongInputTokens
	current.LongOutputTokens += aggregate.LongOutputTokens
	current.LongCachedTokens += aggregate.LongCachedTokens
	current.LongCacheReadTokens += aggregate.LongCacheReadTokens
	current.LongCacheWriteTokens += aggregate.LongCacheWriteTokens
	if current.FirstSeenMS == 0 || aggregate.FirstSeenMS < current.FirstSeenMS {
		current.FirstSeenMS = aggregate.FirstSeenMS
	}
	if aggregate.LastSeenMS > current.LastSeenMS {
		current.LastSeenMS = aggregate.LastSeenMS
		current.AccountLabel = aggregate.AccountLabel
	}
	s.state.Aggregates[key] = current
	s.mu.Unlock()
	s.markDirty()
}

func aggregateFromRecord(record coreusage.Record) (Aggregate, string) {
	provider := normalizedOr(record.Provider, "unknown")
	model := normalizedOr(record.Model, "unknown")
	authIndex := strings.TrimSpace(record.AuthIndex)
	accountKey := stableAccountKey(provider, authIndex, record.AuthID, record.Source)
	accountLabel := safeAccountLabel(record.Source, record.AuthType, authIndex, accountKey)
	serviceTier := strings.TrimSpace(record.ResponseServiceTier)
	if serviceTier == "" {
		serviceTier = strings.TrimSpace(record.ServiceTier)
	}
	serviceTier = normalizedOr(serviceTier, "default")
	timestamp := record.RequestedAt
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	timestampMS := timestamp.UnixMilli()
	totalTokens := record.Detail.TotalTokens
	if totalTokens == 0 {
		totalTokens = record.Detail.InputTokens + record.Detail.OutputTokens
	}

	aggregate := Aggregate{
		AccountKey:       accountKey,
		AuthIndex:        authIndex,
		AccountLabel:     accountLabel,
		Provider:         provider,
		ExecutorType:     strings.TrimSpace(record.ExecutorType),
		AuthType:         strings.TrimSpace(record.AuthType),
		Model:            model,
		ServiceTier:      serviceTier,
		FailureCalls:     boolInt64(record.Failed),
		InputTokens:      maxInt64(record.Detail.InputTokens, 0),
		OutputTokens:     maxInt64(record.Detail.OutputTokens, 0),
		ReasoningTokens:  maxInt64(record.Detail.ReasoningTokens, 0),
		CachedTokens:     maxInt64(record.Detail.CachedTokens, 0),
		CacheReadTokens:  maxInt64(record.Detail.CacheReadTokens, 0),
		CacheWriteTokens: maxInt64(record.Detail.CacheCreationTokens, 0),
		TotalTokens:      maxInt64(totalTokens, 0),
		FirstSeenMS:      timestampMS,
		LastSeenMS:       timestampMS,
	}
	if aggregate.InputTokens > longContextInputThreshold {
		aggregate.LongInputTokens = aggregate.InputTokens
		aggregate.LongOutputTokens = aggregate.OutputTokens
		aggregate.LongCachedTokens = aggregate.CachedTokens
		aggregate.LongCacheReadTokens = aggregate.CacheReadTokens
		aggregate.LongCacheWriteTokens = aggregate.CacheWriteTokens
	}
	key := strings.Join([]string{accountKey, provider, model, serviceTier}, "\x1f")
	return aggregate, key
}

func stableAccountKey(provider, authIndex, authID, source string) string {
	if authIndex = strings.TrimSpace(authIndex); authIndex != "" {
		return provider + ":" + authIndex
	}
	seed := strings.TrimSpace(authID)
	if seed == "" {
		seed = strings.TrimSpace(source)
	}
	if seed == "" {
		seed = "unknown"
	}
	sum := sha256.Sum256([]byte(provider + ":" + seed))
	return provider + ":fallback:" + hex.EncodeToString(sum[:8])
}

func safeAccountLabel(source, authType, authIndex, accountKey string) string {
	source = strings.TrimSpace(source)
	identity := strings.ToLower(strings.TrimSpace(authType))
	apiKeyAuth := identity == "apikey" || strings.Contains(identity, "api_key") || strings.Contains(identity, "api-key")
	if source != "" && !apiKeyAuth && safeLabel(source) {
		return source
	}
	identifier := strings.TrimSpace(authIndex)
	if identifier == "" {
		identifier = accountKey
	}
	if len(identifier) > 10 {
		identifier = identifier[:10]
	}
	if apiKeyAuth {
		return "API key " + identifier
	}
	return "Account " + identifier
}

func safeLabel(value string) bool {
	if len(value) > 160 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func boolInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func normalizedOr(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

// UpsertPrice creates or replaces one model price and persists it immediately.
func (s *Store) UpsertPrice(price ModelPrice) error {
	if s == nil {
		return errors.New("usage statistics store unavailable")
	}
	price.Model = strings.TrimSpace(price.Model)
	if price.Model == "" || len(price.Model) > maxModelNameLength {
		return errors.New("model must contain between 1 and 256 characters")
	}
	if price.InputMicrosPerMillion < 0 || price.OutputMicrosPerMillion < 0 || price.CacheReadMicrosPerMillion < 0 || price.CacheWriteMicrosPerMillion < 0 {
		return errors.New("model prices must not be negative")
	}
	price.Source = normalizedOr(price.Source, "manual")
	price.UpdatedAtMS = time.Now().UnixMilli()
	key := normalizeModelKey(price.Model)

	s.mu.Lock()
	previous, existed := s.state.Prices[key]
	s.state.Prices[key] = price
	s.mu.Unlock()
	if errSave := s.saveSnapshot(); errSave != nil {
		s.mu.Lock()
		if existed {
			s.state.Prices[key] = previous
		} else {
			delete(s.state.Prices, key)
		}
		s.mu.Unlock()
		return errSave
	}
	return nil
}

// DeletePrice removes one model price and persists the change immediately.
func (s *Store) DeletePrice(model string) error {
	if s == nil {
		return errors.New("usage statistics store unavailable")
	}
	key := normalizeModelKey(model)
	if key == "" {
		return errors.New("model is required")
	}
	s.mu.Lock()
	previous, existed := s.state.Prices[key]
	if !existed {
		s.mu.Unlock()
		return os.ErrNotExist
	}
	delete(s.state.Prices, key)
	s.mu.Unlock()
	if errSave := s.saveSnapshot(); errSave != nil {
		s.mu.Lock()
		s.state.Prices[key] = previous
		s.mu.Unlock()
		return errSave
	}
	return nil
}

// ListPrices returns model prices sorted by model name.
func (s *Store) ListPrices() []ModelPrice {
	if s == nil {
		return []ModelPrice{}
	}
	s.mu.RLock()
	prices := make([]ModelPrice, 0, len(s.state.Prices))
	for _, price := range s.state.Prices {
		prices = append(prices, price)
	}
	s.mu.RUnlock()
	sort.Slice(prices, func(left, right int) bool {
		return strings.ToLower(prices[left].Model) < strings.ToLower(prices[right].Model)
	})
	return prices
}

// Summary computes current-price account and model cost estimates.
func (s *Store) Summary() Summary {
	if s == nil {
		return Summary{Accounts: []AccountSummary{}, Models: []ModelUsageSummary{}, UnpricedModels: []string{}}
	}
	s.mu.RLock()
	aggregates := make([]Aggregate, 0, len(s.state.Aggregates))
	for _, aggregate := range s.state.Aggregates {
		aggregates = append(aggregates, aggregate)
	}
	prices := make(map[string]ModelPrice, len(s.state.Prices))
	for key, price := range s.state.Prices {
		prices[key] = price
	}
	s.mu.RUnlock()

	accountMap := make(map[string]*AccountSummary)
	accountModelMaps := make(map[string]map[string]*ModelUsageSummary)
	modelMap := make(map[string]*ModelUsageSummary)
	unpriced := make(map[string]struct{})
	result := Summary{
		Enabled:        s.enabled.Load(),
		Accounts:       []AccountSummary{},
		Models:         []ModelUsageSummary{},
		UnpricedModels: []string{},
	}
	for _, aggregate := range aggregates {
		price, priced := findModelPrice(aggregate.Model, prices)
		costMicros := int64(0)
		if priced {
			costMicros = costMicrosForAggregate(aggregate, price)
		} else {
			unpriced[aggregate.Model] = struct{}{}
		}

		account := accountMap[aggregate.AccountKey]
		if account == nil {
			account = &AccountSummary{
				AccountKey:   aggregate.AccountKey,
				AuthIndex:    aggregate.AuthIndex,
				AccountLabel: aggregate.AccountLabel,
				Provider:     aggregate.Provider,
				AuthType:     aggregate.AuthType,
				FirstSeenMS:  aggregate.FirstSeenMS,
				LastSeenMS:   aggregate.LastSeenMS,
				Models:       []ModelUsageSummary{},
			}
			accountMap[aggregate.AccountKey] = account
			accountModelMaps[aggregate.AccountKey] = make(map[string]*ModelUsageSummary)
		}
		if aggregate.FirstSeenMS > 0 && (account.FirstSeenMS == 0 || aggregate.FirstSeenMS < account.FirstSeenMS) {
			account.FirstSeenMS = aggregate.FirstSeenMS
		}
		if aggregate.LastSeenMS > account.LastSeenMS {
			account.LastSeenMS = aggregate.LastSeenMS
			account.AccountLabel = aggregate.AccountLabel
		}
		addAggregateTotals(&account.Totals, aggregate, costMicros, priced)
		accountModel := accountModelMaps[aggregate.AccountKey][aggregate.Model]
		if accountModel == nil {
			accountModel = &ModelUsageSummary{Model: aggregate.Model, Priced: priced}
			accountModelMaps[aggregate.AccountKey][aggregate.Model] = accountModel
		}
		accountModel.Priced = accountModel.Priced || priced
		addAggregateTotals(&accountModel.Totals, aggregate, costMicros, priced)

		model := modelMap[aggregate.Model]
		if model == nil {
			model = &ModelUsageSummary{Model: aggregate.Model, Priced: priced}
			modelMap[aggregate.Model] = model
		}
		model.Priced = model.Priced || priced
		addAggregateTotals(&model.Totals, aggregate, costMicros, priced)
		addAggregateTotals(&result.Totals, aggregate, costMicros, priced)
		if aggregate.LastSeenMS > result.UpdatedAtMS {
			result.UpdatedAtMS = aggregate.LastSeenMS
		}
	}

	for _, account := range accountMap {
		for _, model := range accountModelMaps[account.AccountKey] {
			account.Models = append(account.Models, *model)
		}
		sort.Slice(account.Models, func(left, right int) bool {
			return account.Models[left].CostMicros > account.Models[right].CostMicros
		})
		result.Accounts = append(result.Accounts, *account)
	}
	sort.Slice(result.Accounts, func(left, right int) bool {
		if result.Accounts[left].CostMicros == result.Accounts[right].CostMicros {
			return result.Accounts[left].TotalTokens > result.Accounts[right].TotalTokens
		}
		return result.Accounts[left].CostMicros > result.Accounts[right].CostMicros
	})
	for _, model := range modelMap {
		result.Models = append(result.Models, *model)
	}
	sort.Slice(result.Models, func(left, right int) bool {
		if result.Models[left].CostMicros == result.Models[right].CostMicros {
			return result.Models[left].TotalTokens > result.Models[right].TotalTokens
		}
		return result.Models[left].CostMicros > result.Models[right].CostMicros
	})
	for model := range unpriced {
		result.UnpricedModels = append(result.UnpricedModels, model)
	}
	sort.Strings(result.UnpricedModels)
	return result
}

func addAggregateTotals(totals *Totals, aggregate Aggregate, costMicros int64, priced bool) {
	if totals == nil {
		return
	}
	totals.Calls += aggregate.Calls
	totals.SuccessCalls += aggregate.SuccessCalls
	totals.FailureCalls += aggregate.FailureCalls
	totals.InputTokens += aggregate.InputTokens
	totals.OutputTokens += aggregate.OutputTokens
	totals.ReasoningTokens += aggregate.ReasoningTokens
	totals.CacheReadTokens += maxInt64(aggregate.CacheReadTokens, aggregate.CachedTokens)
	totals.CacheWriteTokens += aggregate.CacheWriteTokens
	totals.TotalTokens += aggregate.TotalTokens
	totals.CostMicros += costMicros
	if !priced {
		totals.UnpricedCalls += aggregate.Calls
	}
}

func findModelPrice(model string, prices map[string]ModelPrice) (ModelPrice, bool) {
	key := normalizeModelKey(model)
	if price, ok := prices[key]; ok {
		return price, true
	}
	tail := normalizedModelTail(model)
	var matched ModelPrice
	matches := 0
	for priceKey, price := range prices {
		if normalizedModelTail(priceKey) != tail {
			continue
		}
		matched = price
		matches++
	}
	return matched, matches == 1
}

func normalizeModelKey(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func (s *Store) load(ctx context.Context) error {
	data, errLoad := s.storage.Load(ctx)
	if errLoad != nil {
		return fmt.Errorf("load usage statistics: %w", errLoad)
	}
	if len(data) == 0 {
		return nil
	}
	state, errDecode := decodePersistentState(data)
	if errDecode != nil {
		return errDecode
	}
	s.state = state
	return nil
}

func decodePersistentState(data []byte) (persistentState, error) {
	var state persistentState
	if errUnmarshal := json.Unmarshal(data, &state); errUnmarshal != nil {
		return persistentState{}, fmt.Errorf("decode usage statistics: %w", errUnmarshal)
	}
	if state.Version != storageVersion {
		return persistentState{}, fmt.Errorf("unsupported usage statistics version %d", state.Version)
	}
	if state.Prices == nil {
		state.Prices = make(map[string]ModelPrice)
	}
	if state.Aggregates == nil {
		state.Aggregates = make(map[string]Aggregate)
	}
	return state, nil
}

func (s *Store) run() {
	defer close(s.done)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	dirty := false
	for {
		select {
		case <-s.dirty:
			dirty = true
		case <-ticker.C:
			if dirty {
				if errSave := s.saveSnapshot(); errSave != nil {
					log.WithError(errSave).Error("usage statistics: failed to persist aggregates")
				} else {
					dirty = false
				}
			}
		case <-s.stop:
			if errSave := s.saveSnapshot(); errSave != nil {
				log.WithError(errSave).Error("usage statistics: failed to persist aggregates during shutdown")
			}
			return
		}
	}
}

func (s *Store) markDirty() {
	select {
	case s.dirty <- struct{}{}:
	default:
	}
}

func (s *Store) saveSnapshot() error {
	if s == nil {
		return nil
	}
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	s.mu.RLock()
	data, errMarshal := json.MarshalIndent(s.state, "", "  ")
	s.mu.RUnlock()
	if errMarshal != nil {
		return fmt.Errorf("encode usage statistics: %w", errMarshal)
	}
	if errSave := s.storage.Save(context.Background(), data); errSave != nil {
		return fmt.Errorf("save usage statistics: %w", errSave)
	}
	return nil
}

// Close flushes pending changes and stops the background worker.
func (s *Store) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		close(s.stop)
		<-s.done
		if errClose := s.storage.Close(); errClose != nil {
			log.WithError(errClose).Error("usage statistics: failed to close storage")
		}
	})
}
