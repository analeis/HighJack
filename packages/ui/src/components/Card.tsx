import { splitProps } from 'solid-js';
import type { JSX } from 'solid-js';

export interface CardProps extends JSX.HTMLAttributes<HTMLDivElement> {
  /** `raised` lifts the card off the felt with shadow. */
  raised?: boolean;
  interactive?: boolean;
}

/**
 * The base surface primitive: a card lying on the table.
 * Interactive cards communicate hoverability via border + lift, not color alone.
 */
export function Card(props: CardProps) {
  const [local, rest] = splitProps(props, ['class', 'raised', 'interactive']);
  return (
    <div
      class={`rounded-lg border border-line-subtle bg-surface-raised ${local.raised ? 'shadow-raised' : 'shadow-card'} ${local.interactive ? 'transition-all duration-normal ease-out-hj hover:-translate-y-0.5 hover:border-line-strong' : ''} ${local.class ?? ''}`}
      {...rest}
    />
  );
}

export function CardHeader(props: JSX.HTMLAttributes<HTMLDivElement>) {
  const [local, rest] = splitProps(props, ['class']);
  return (
    <div
      class={`flex flex-col gap-1 border-b border-line-subtle px-5 py-4 ${local.class ?? ''}`}
      {...rest}
    />
  );
}

export function CardTitle(props: JSX.HTMLAttributes<HTMLHeadingElement>) {
  const [local, rest] = splitProps(props, ['class']);
  return (
    <h3
      class={`m-0 font-display text-lg font-bold text-cream-100 ${local.class ?? ''}`}
      {...rest}
    />
  );
}

export function CardBody(props: JSX.HTMLAttributes<HTMLDivElement>) {
  const [local, rest] = splitProps(props, ['class']);
  return (
    <div
      class={`px-5 py-4 text-sm leading-relaxed text-muted-400 ${local.class ?? ''}`}
      {...rest}
    />
  );
}
