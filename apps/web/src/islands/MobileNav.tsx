import { createSignal, onCleanup, onMount, Show, For } from 'solid-js';
import { isServer } from 'solid-js/web';

export interface NavItem {
  href: string;
  label: string;
}

/**
 * Responsive navigation.
 *
 * Desktop: horizontal links. Mobile (<md): a disclosure panel toggled by
 * a hamburger button with proper aria-expanded/aria-controls wiring and
 * Escape-to-close. The current route is marked with aria-current="page".
 */
export function MobileNav(props: { items: NavItem[]; currentPath: string }) {
  const [open, setOpen] = createSignal(false);
  const panelId = 'mobile-nav-panel';
  let buttonRef: HTMLButtonElement | undefined;

  const toggle = () => setOpen(!open());
  const close = () => setOpen(false);

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Escape' && open()) {
      close();
      buttonRef?.focus();
    }
  };

  onMount(() => {
    if (isServer) return;
    document.addEventListener('keydown', onKeyDown);
  });
  onCleanup(() => {
    if (isServer) return;
    document.removeEventListener('keydown', onKeyDown);
  });

  return (
    <div class="md:hidden">
      <button
        ref={buttonRef}
        type="button"
        aria-expanded={open()}
        aria-controls={panelId}
        aria-label={open() ? 'Close menu' : 'Open menu'}
        onClick={toggle}
        class="inline-flex h-10 w-10 cursor-pointer items-center justify-center rounded-md border border-line-subtle bg-surface-raised text-cream-100"
      >
        <svg
          viewBox="0 0 24 24"
          width="20"
          height="20"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          aria-hidden="true"
        >
          <Show when={!open()} fallback={<path d="M5 5l14 14M19 5L5 19" />}>
            <path d="M4 7h16M4 12h16M4 17h16" />
          </Show>
        </svg>
      </button>
      <Show when={open()}>
        <nav
          id={panelId}
          aria-label="Mobile"
          class="absolute inset-x-0 top-full z-40 mt-2 border-y border-line-subtle bg-felt-800/95 px-4 py-3 shadow-raised backdrop-blur"
        >
          <ul class="flex flex-col gap-1">
            <For each={props.items}>
              {(item) => (
                <li>
                  <a
                    href={item.href}
                    onClick={close}
                    aria-current={props.currentPath === item.href ? 'page' : undefined}
                    class={`block rounded-md px-3 py-2.5 text-md font-bold ${
                      props.currentPath === item.href
                        ? 'bg-felt-700 text-gold-400'
                        : 'text-muted-400 hover:bg-felt-700 hover:text-cream-100'
                    }`}
                  >
                    {item.label}
                  </a>
                </li>
              )}
            </For>
          </ul>
        </nav>
      </Show>
    </div>
  );
}
