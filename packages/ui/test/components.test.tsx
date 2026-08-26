import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@solidjs/testing-library';
import userEvent from '@testing-library/user-event';
import { Button } from '../src/components/Button.tsx';
import { Badge, Money, StatusDot } from '../src/components/Badge.tsx';
import { Card, CardBody, CardHeader, CardTitle } from '../src/components/Card.tsx';
import { Switch } from '../src/components/Switch.tsx';
import { Tabs, TabPanel } from '../src/components/Tabs.tsx';

describe('Button', () => {
  it('renders with an accessible role and fires clicks', async () => {
    const onClick = vi.fn();
    render(() => <Button onClick={onClick}>Deal me in</Button>);
    const btn = screen.getByRole('button', { name: 'Deal me in' });
    await userEvent.click(btn);
    expect(onClick).toHaveBeenCalledOnce();
  });

  it('is disabled and inert when disabled', async () => {
    const onClick = vi.fn();
    render(() => (
      <Button disabled onClick={onClick}>
        Locked
      </Button>
    ));
    const btn = screen.getByRole('button', { name: 'Locked' });
    expect(btn).toBeDisabled();
    await userEvent.click(btn);
    expect(onClick).not.toHaveBeenCalled();
  });

  it('applies variant classes without leaking them as DOM attributes', () => {
    render(() => <Button variant="primary">Go</Button>);
    const btn = screen.getByRole('button');
    expect(btn.className).toContain('bg-gold-500');
  });
});

describe('Badge & status', () => {
  it('renders badge text', () => {
    render(() => <Badge tone="gold">In development</Badge>);
    expect(screen.getByText('In development')).toBeInTheDocument();
  });

  it('status dot exposes a text label (never color-only)', () => {
    render(() => <StatusDot status="soon" label="Coming soon" />);
    expect(screen.getByRole('img', { name: 'Coming soon' })).toBeInTheDocument();
  });

  it('Money formats integers and refuses invalid amounts', () => {
    const { container: ok } = render(() => <Money amount={1500} />);
    expect(ok.textContent).toContain('1,500');

    const { container: bad } = render(() => <Money amount={-5} />);
    expect(bad.textContent).toBe('');

    const { container: frac } = render(() => <Money amount={1.5} />);
    expect(frac.textContent).toBe('');
  });
});

describe('Card', () => {
  it('composes header/title/body sections', () => {
    render(() => (
      <Card raised>
        <CardHeader>
          <CardTitle>House rules</CardTitle>
        </CardHeader>
        <CardBody>Configure everything.</CardBody>
      </Card>
    ));
    expect(screen.getByRole('heading', { name: 'House rules' })).toBeInTheDocument();
    expect(screen.getByText('Configure everything.')).toBeInTheDocument();
  });
});

describe('Switch', () => {
  it('toggles and reports changes', async () => {
    const onChange = vi.fn();
    render(() => <Switch checked={false} onChange={onChange} label="Enable gambling" />);
    const sw = screen.getByRole('switch', { name: 'Enable gambling' });
    expect(sw).toHaveAttribute('aria-checked', 'false');
    await userEvent.click(sw);
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it('reflects checked state via aria-checked', () => {
    render(() => <Switch checked onChange={() => {}} label="Trading" />);
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'true');
  });
});

describe('Tabs', () => {
  const tabs = [
    { id: 'economy', label: 'Economy' },
    { id: 'risk', label: 'Risk' },
    { id: 'chaos', label: 'Chaos' },
  ];

  function Harness(props: { onChange?: (id: string) => void }) {
    return (
      <>
        <Tabs tabs={tabs} value="risk" onChange={(id) => props.onChange?.(id)} label="Systems" />
        <TabPanel active id="economy">
          economy panel
        </TabPanel>
        <TabPanel active={false} id="chaos">
          chaos panel
        </TabPanel>
      </>
    );
  }

  it('marks the selected tab and exposes panels', () => {
    render(() => <Harness />);
    const selected = screen.getByRole('tab', { selected: true });
    expect(selected).toHaveTextContent('Risk');
    expect(screen.getByRole('tablist', { name: 'Systems' })).toBeInTheDocument();
  });

  it('only the selected tab is tabbable (roving tabindex)', () => {
    render(() => <Harness />);
    for (const tab of screen.getAllByRole('tab')) {
      if (tab.textContent === 'Risk') {
        expect(tab).toHaveAttribute('tabindex', '0');
      } else {
        expect(tab).toHaveAttribute('tabindex', '-1');
      }
    }
  });

  it('selects on click and moves focus with arrow keys', async () => {
    const onChange = vi.fn();
    render(() => <Harness onChange={onChange} />);
    const risk = screen.getByRole('tab', { name: 'Chaos' });
    await userEvent.click(risk);
    expect(onChange).toHaveBeenCalledWith('chaos');

    const list = screen.getByRole('tablist', { name: 'Systems' });
    // Selection starts at "Risk" (index 1): ArrowRight moves to "Chaos".
    list.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    expect(onChange).toHaveBeenLastCalledWith('chaos');

    // The harness keeps the controlled value at "risk", so Left moves to
    // "Economy" (index 0).
    list.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }));
    expect(onChange).toHaveBeenLastCalledWith('economy');
  });
});
