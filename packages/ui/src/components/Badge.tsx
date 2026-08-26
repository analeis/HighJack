import { Show, splitProps } from 'solid-js';
import type { JSX } from 'solid-js';

type Tone = 'neutral' | 'gold' | 'teal' | 'crimson' | 'violet';

export interface BadgeProps extends JSX.HTMLAttributes<HTMLSpanElement> {
  tone?: Tone;
}

const toneClasses: Record<Tone, string> = {
  neutral: 'bg-felt-700 text-muted-400 border-line-subtle',
  gold: 'bg-gold-700/25 text-gold-400 border-gold-700/60',
  teal: 'bg-teal-600/20 text-teal-400 border-teal-600/50',
  crimson: 'bg-crimson-600/20 text-crimson-400 border-crimson-600/50',
  violet: 'bg-violet-600/20 text-violet-400 border-violet-600/50',
};

/** Compact status/category label. Never the only carrier of meaning. */
export function Badge(props: BadgeProps) {
  const [local, rest] = splitProps(props, ['tone', 'class']);
  return (
    <span
      class={`inline-flex items-center gap-1 rounded-pill border px-2.5 py-0.5 text-xs font-bold uppercase tracking-wider ${toneClasses[local.tone ?? 'neutral']} ${local.class ?? ''}`}
      {...rest}
    />
  );
}

/** Status dot with mandatory screen-reader text (no color-only status). */
export function StatusDot(props: { status: 'live' | 'soon' | 'planned'; label: string }) {
  const color = () =>
    props.status === 'live'
      ? 'bg-teal-500'
      : props.status === 'soon'
        ? 'bg-gold-500'
        : 'bg-muted-500';
  const pulse = () => (props.status === 'live' ? 'animate-pulse' : '');
  return (
    <span class="relative inline-flex h-2 w-2" role="img" aria-label={props.label}>
      <span
        class={`absolute inline-flex h-full w-full rounded-full opacity-60 ${color()} ${pulse()}`}
      />
      <span class={`relative inline-flex h-2 w-2 rounded-full ${color()}`} />
    </span>
  );
}

/** Money display: always integer, always tabular mono, never negative here. */
export function Money(props: { amount: number; class?: string }) {
  const formatted = () => props.amount.toLocaleString('en-US');
  return (
    <Show when={Number.isSafeInteger(props.amount) && props.amount >= 0}>
      <span class={`hj-numerals ${props.class ?? ''}`}>{formatted()}</span>
    </Show>
  );
}
