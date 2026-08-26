import type { JSX } from 'solid-js';
import { splitProps } from 'solid-js';

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger';
type Size = 'sm' | 'md' | 'lg';

export interface ButtonProps extends JSX.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
}

const variantClasses: Record<Variant, string> = {
  primary: 'bg-gold-500 text-felt-950 hover:bg-gold-400 active:bg-gold-600 shadow-card font-bold',
  secondary:
    'bg-surface-raised text-cream-100 border border-line-strong hover:bg-surface-overlay hover:border-gold-700 active:bg-felt-800',
  ghost: 'bg-transparent text-muted-400 hover:text-cream-100 hover:bg-felt-800',
  danger: 'bg-crimson-600 text-cream-100 hover:bg-crimson-500 active:bg-crimson-600',
};

const sizeClasses: Record<Size, string> = {
  sm: 'text-xs px-3 py-1.5 gap-1.5',
  md: 'text-sm px-4 py-2 gap-2',
  lg: 'text-md px-6 py-3 gap-2',
};

/**
 * The single button primitive. Interactive states are always visible via
 * more than color alone (elevation/border shifts accompany hue changes).
 */
export function Button(props: ButtonProps) {
  const [local, rest] = splitProps(props, ['variant', 'size', 'class']);
  const variant = () => local.variant ?? 'secondary';
  const size = () => local.size ?? 'md';
  return (
    <button
      class={`inline-flex cursor-pointer items-center justify-center rounded-md font-body transition-all duration-fast ease-out-hj disabled:pointer-events-none disabled:opacity-45 ${variantClasses[variant()]} ${sizeClasses[size()]} ${local.class ?? ''}`}
      {...rest}
    />
  );
}
