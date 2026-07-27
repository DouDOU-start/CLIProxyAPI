const numberFormatter = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 });
const compactNumberFormatter = new Intl.NumberFormat(undefined, {
  notation: 'compact',
  maximumFractionDigits: 2,
});

export const formatUsageTokens = (value: number) =>
  compactNumberFormatter.format(Math.max(0, value));

export const formatUsageNumber = (value: number) => numberFormatter.format(Math.max(0, value));

export const formatUsageCost = (micros: number) => {
  const value = Math.max(0, micros) / 1_000_000;
  const maximumFractionDigits = value > 0 && value < 0.01 ? 6 : value < 10 ? 4 : 2;
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits,
  }).format(value);
};
