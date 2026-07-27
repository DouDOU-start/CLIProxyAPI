import { describe, expect, test } from 'bun:test';
import { createElement, useState } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { parse as parseYaml } from 'yaml';
import { useVisualConfig } from '../src/hooks/useVisualConfig';

const existingYaml = `remote-management:
  allow-remote: false
  email: admin@example.com
  password: $2b$10$existingHash
`;

describe('visual config management account', () => {
  test('does not expose a persisted password hash in the editor', () => {
    function Harness() {
      const visualConfig = useVisualConfig();
      const [loaded, setLoaded] = useState(false);
      if (!loaded) {
        visualConfig.loadVisualValuesFromYaml(existingYaml);
        setLoaded(true);
        return null;
      }
      return createElement(
        'pre',
        null,
        `${visualConfig.visualValues.rmEmail}|${visualConfig.visualValues.rmPassword}`
      );
    }

    const markup = renderToStaticMarkup(createElement(Harness));
    expect(markup.slice('<pre>'.length, -'</pre>'.length)).toBe('admin@example.com|');
  });

  test('keeps the existing password when only the email is changed', () => {
    function Harness() {
      const visualConfig = useVisualConfig();
      const [phase, setPhase] = useState(0);
      if (phase === 0) {
        visualConfig.loadVisualValuesFromYaml(existingYaml);
        setPhase(1);
        return null;
      }
      if (phase === 1) {
        visualConfig.setVisualValues({ rmEmail: 'next@example.com' });
        setPhase(2);
        return null;
      }
      return createElement('pre', null, visualConfig.applyVisualChangesToYaml(existingYaml));
    }

    const markup = renderToStaticMarkup(createElement(Harness));
    const merged = parseYaml(markup.slice('<pre>'.length, -'</pre>'.length));
    expect(merged['remote-management'].email).toBe('next@example.com');
    expect(merged['remote-management'].password).toBe('$2b$10$existingHash');
    expect(merged['remote-management']['allow-remote']).toBeUndefined();
  });

  test('writes a new password only after it is entered', () => {
    function Harness() {
      const visualConfig = useVisualConfig();
      const [phase, setPhase] = useState(0);
      if (phase === 0) {
        visualConfig.loadVisualValuesFromYaml(existingYaml);
        setPhase(1);
        return null;
      }
      if (phase === 1) {
        visualConfig.setVisualValues({ rmPassword: 'replacement-password-456' });
        setPhase(2);
        return null;
      }
      return createElement('pre', null, visualConfig.applyVisualChangesToYaml(existingYaml));
    }

    const markup = renderToStaticMarkup(createElement(Harness));
    const merged = parseYaml(markup.slice('<pre>'.length, -'</pre>'.length));
    expect(merged['remote-management'].password).toBe('replacement-password-456');
  });
});
