import { For } from 'solid-js';
import type { JSX } from 'solid-js';

export interface TabDef {
  id: string;
  label: string;
}

export interface TabsProps {
  tabs: readonly TabDef[];
  value: string;
  onChange: (id: string) => void;
  /** Accessible label for the tablist. */
  label: string;
  class?: string;
}

/**
 * Roving-tabindex tab strip implementing the WAI-ARIA Tabs pattern:
 * Left/Right move focus and selection, Home/End jump, Enter/Space select.
 */
export function Tabs(props: TabsProps & { children?: JSX.Element }) {
  let listRef: HTMLDivElement | undefined;

  const move = (from: number, delta: number) => {
    const count = props.tabs.length;
    if (count === 0) return;
    const next = (from + delta + count) % count;
    props.onChange(props.tabs[next]!.id);
    const btn = listRef?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next];
    btn?.focus();
  };

  const onKeyDown = (e: KeyboardEvent) => {
    const index = props.tabs.findIndex((tab) => tab.id === props.value);
    switch (e.key) {
      case 'ArrowRight':
        e.preventDefault();
        move(index, 1);
        break;
      case 'ArrowLeft':
        e.preventDefault();
        move(index, -1);
        break;
      case 'Home':
        e.preventDefault();
        props.onChange(props.tabs[0]!.id);
        listRef?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[0]?.focus();
        break;
      case 'End': {
        e.preventDefault();
        const last = props.tabs.length - 1;
        props.onChange(props.tabs[last]!.id);
        listRef?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[last]?.focus();
        break;
      }
    }
  };

  return (
    <div
      ref={listRef}
      role="tablist"
      aria-label={props.label}
      class={`flex gap-1 ${props.class ?? ''}`}
      onKeyDown={onKeyDown}
    >
      <For each={props.tabs}>
        {(tab) => (
          <button
            type="button"
            role="tab"
            id={`tab-${tab.id}`}
            aria-selected={props.value === tab.id}
            aria-controls={`panel-${tab.id}`}
            tabIndex={props.value === tab.id ? 0 : -1}
            onClick={() => props.onChange(tab.id)}
            class={`cursor-pointer rounded-md px-3.5 py-2 text-sm font-bold transition-colors duration-fast ${
              props.value === tab.id
                ? 'bg-felt-700 text-gold-400 shadow-card'
                : 'text-muted-400 hover:bg-felt-800 hover:text-cream-100'
            }`}
          >
            {tab.label}
          </button>
        )}
      </For>
    </div>
  );
}

/** Panel companion; render with `aria-labelledby="tab-{id}"`. */
export function TabPanel(props: {
  active: boolean;
  id: string;
  class?: string;
  children?: JSX.Element;
}) {
  return (
    <div
      role="tabpanel"
      id={`panel-${props.id}`}
      aria-labelledby={`tab-${props.id}`}
      hidden={!props.active}
      class={props.class}
    >
      {props.children}
    </div>
  );
}
