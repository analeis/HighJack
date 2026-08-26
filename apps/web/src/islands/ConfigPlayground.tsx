import { createMemo, createSignal, For, Show } from 'solid-js';
import {
  DEFAULT_CONFIG,
  canonicalJson,
  validateConfig,
  type ConfigIssue,
  type GameConfig,
} from '@highjack/protocol';
import { Badge, Switch } from '@highjack/ui';

/**
 * Interactive configuration preview.
 *
 * This is real, not decorative: it builds an actual wire-format GameConfig
 * using @highjack/protocol — the same schema the Go engine validates and
 * hashes. Toggling systems updates the JSON preview and runs the full
 * structural + semantic validation client-side, including deliberately
 * reachable invalid states (e.g. poker on with gambling off) so visitors
 * can see how HighJack refuses impossible rulesets.
 */
export function ConfigPlayground() {
  const [config, setConfig] = createSignal<GameConfig>({ ...DEFAULT_CONFIG });

  const setFlag = (path: string[], value: boolean) => {
    setConfig((prev) => {
      // Paths are fixed two-level keys; keep it explicit and type-safe.
      switch (path.join('.')) {
        case 'trading.enabled':
          return { ...prev, trading: { enabled: value } };
        case 'auctions.enabled':
          return { ...prev, auctions: { enabled: value } };
        case 'gambling.enabled':
          return value
            ? { ...prev, gambling: { ...prev.gambling, enabled: true } }
            : {
                ...prev,
                gambling: { enabled: false, poker: false, blackjack: false, casino: false },
              };
        case 'gambling.poker':
          return { ...prev, gambling: { ...prev.gambling, poker: value } };
        case 'gambling.blackjack':
          return { ...prev, gambling: { ...prev.gambling, blackjack: value } };
        case 'gambling.casino':
          return { ...prev, gambling: { ...prev.gambling, casino: value } };
        case 'carnival.enabled':
          return { ...prev, carnival: { enabled: value } };
        case 'sports.enabled':
          return { ...prev, sports: { enabled: value } };
        case 'cards.enabled':
          return { ...prev, cards: { enabled: value } };
        default:
          return prev;
      }
    });
  };

  const result = createMemo(() => validateConfig(config()));
  const issues = createMemo<ConfigIssue[]>(() => {
    const r = result();
    return r.ok ? [] : [...r.issues];
  });
  const pretty = createMemo(() => {
    try {
      return JSON.stringify(JSON.parse(canonicalJson(config())), null, 2);
    } catch {
      return '';
    }
  });

  return (
    <div class="grid gap-4 lg:grid-cols-[1fr_1.2fr]">
      <div class="flex flex-col gap-3 rounded-lg border border-line-subtle bg-surface-raised p-4 shadow-card sm:p-5">
        <h3 class="m-0 font-display text-md font-bold text-cream-100">Rule modules</h3>
        <For
          each={[
            {
              key: 'trading.enabled',
              label: 'Trading & negotiation',
              value: () => config().trading.enabled,
            },
            { key: 'auctions.enabled', label: 'Auctions', value: () => config().auctions.enabled },
            {
              key: 'gambling.enabled',
              label: 'Gambling tables (master switch)',
              value: () => config().gambling.enabled,
            },
            { key: 'gambling.poker', label: 'Poker nights', value: () => config().gambling.poker },
            {
              key: 'gambling.blackjack',
              label: 'Blackjack pits',
              value: () => config().gambling.blackjack,
            },
            {
              key: 'gambling.casino',
              label: 'Casino floor',
              value: () => config().gambling.casino,
            },
            { key: 'carnival.enabled', label: 'Carnival', value: () => config().carnival.enabled },
            {
              key: 'sports.enabled',
              label: 'Sports minigames',
              value: () => config().sports.enabled,
            },
            { key: 'cards.enabled', label: 'Treasure cards', value: () => config().cards.enabled },
          ]}
        >
          {(item) => (
            <div class="flex items-center justify-between gap-3 rounded-md px-1 py-1.5 hover:bg-felt-800">
              <span class="text-sm text-cream-300">{item.label}</span>
              <Switch
                checked={item.value()}
                onChange={(v) => setFlag(item.key.split('.'), v)}
                label={item.label}
              />
            </div>
          )}
        </For>
      </div>

      <div class="flex min-w-0 flex-col gap-3">
        <div class="flex flex-wrap items-center gap-2">
          <Show when={result().ok} fallback={<Badge tone="crimson">✗ Invalid ruleset</Badge>}>
            <Badge tone="teal">✓ Valid ruleset</Badge>
          </Show>
          <span class="text-xs text-muted-500">
            validated in your browser against the real protocol schema
          </span>
        </div>
        <Show when={!result().ok}>
          <ul
            class="m-0 list-none rounded-md border border-crimson-600/50 bg-crimson-600/10 p-3 pl-6 text-sm text-crimson-400"
            role="alert"
          >
            <For each={issues()}>
              {(issue) => (
                <li class="list-disc">
                  <code class="text-xs">{issue.path || '(root)'}</code>: {issue.message}
                </li>
              )}
            </For>
          </ul>
        </Show>
        <pre
          class="max-h-80 overflow-auto rounded-lg border border-line-subtle bg-felt-950 p-4 text-xs leading-relaxed text-muted-400 shadow-card"
          aria-label="GameConfig JSON preview"
          tabindex={0}
        >
          <code>{pretty()}</code>
        </pre>
        <p class="m-0 text-xs text-muted-500">
          Note: the toggled subsystems are part of the configuration model; gameplay for each
          arrives with its own milestone. The engine already validates and hashes this document
          exactly as shown.
        </p>
      </div>
    </div>
  );
}
