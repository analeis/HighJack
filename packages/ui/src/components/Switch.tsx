import { splitProps } from 'solid-js';
import type { JSX } from 'solid-js';

export interface SwitchProps extends Omit<JSX.ButtonHTMLAttributes<HTMLButtonElement>, 'onChange'> {
  checked: boolean;
  onChange: (checked: boolean) => void;
  /** Accessible name; the visual switch carries no text. */
  label: string;
}

/**
 * Accessible switch built on role="switch".
 * Keyboard: Space toggles; focus ring comes from the token layer.
 */
export function Switch(props: SwitchProps) {
  const [local, rest] = splitProps(props, ['checked', 'onChange', 'label', 'class']);
  return (
    <button
      type="button"
      role="switch"
      aria-checked={local.checked}
      aria-label={local.label}
      onClick={() => local.onChange(!local.checked)}
      class={`relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-pill border transition-colors duration-normal ease-out-hj disabled:pointer-events-none disabled:opacity-45 ${
        local.checked ? 'border-gold-600 bg-gold-600/40' : 'border-line-strong bg-felt-700'
      } ${local.class ?? ''}`}
      {...rest}
    >
      <span
        aria-hidden="true"
        class={`inline-block h-4 w-4 rounded-full transition-all duration-fast ease-out-hj ${
          local.checked ? 'translate-x-6 bg-gold-500' : 'translate-x-1 bg-muted-500'
        }`}
      />
    </button>
  );
}
