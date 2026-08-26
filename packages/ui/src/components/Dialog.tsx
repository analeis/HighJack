import { createEffect, onCleanup, onMount, Show } from 'solid-js';
import type { JSX } from 'solid-js';

export interface DialogProps {
  open: boolean;
  onClose: () => void;
  title: string;
  class?: string;
  children?: JSX.Element;
}

/**
 * Modal dialog on the native <dialog> element: focus trapping, Escape
 * handling, and inert background come from the platform. Esc closes via
 * the native 'cancel' event; backdrop clicks close explicitly.
 */
export function Dialog(props: DialogProps) {
  let ref: HTMLDialogElement | undefined;

  onMount(() => {
    const el = ref!;
    const handleCancel = (e: Event) => {
      e.preventDefault();
      props.onClose();
    };
    el.addEventListener('cancel', handleCancel);
    onCleanup(() => el.removeEventListener('cancel', handleCancel));
  });

  // Keep the imperative dialog state in sync with the prop.
  createEffect(() => {
    const el = ref;
    if (!el) return;
    if (props.open && !el.open) {
      el.showModal();
    } else if (!props.open && el.open) {
      el.close();
    }
  });

  return (
    <dialog
      ref={ref}
      aria-label={props.title}
      onClick={(e) => {
        // Clicks landing on the dialog element itself are backdrop clicks.
        if (e.target === ref) props.onClose();
      }}
      class={`m-auto w-[min(92vw,32rem)] rounded-lg border border-line-strong bg-surface-overlay p-0 text-cream-100 shadow-raised backdrop:bg-felt-950/70 ${props.class ?? ''}`}
    >
      <div class="flex items-center justify-between border-b border-line-subtle px-5 py-4">
        <h2 class="m-0 font-display text-lg font-bold">{props.title}</h2>
        <button
          type="button"
          aria-label="Close dialog"
          onClick={() => props.onClose()}
          class="cursor-pointer rounded-sm border-none bg-transparent p-1 text-muted-400 hover:text-cream-100"
        >
          ✕
        </button>
      </div>
      <Show when={props.open}>
        <div class="px-5 py-4 text-sm leading-relaxed text-muted-400">{props.children}</div>
      </Show>
    </dialog>
  );
}
