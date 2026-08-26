/**
 * @highjack/ui — the HighJack design system.
 *
 * Tokens live in styles/tokens.css (source of truth) and are mapped into
 * Tailwind via styles/index.css. Components here are transport-free Solid
 * primitives shared by the website and the game client's application
 * surface. PixiJS rendering never enters this package.
 */
export { Button, type ButtonProps } from './components/Button.tsx';
export { Badge, StatusDot, Money, type BadgeProps } from './components/Badge.tsx';
export { Card, CardHeader, CardTitle, CardBody, type CardProps } from './components/Card.tsx';
export { Switch, type SwitchProps } from './components/Switch.tsx';
export { Tabs, TabPanel, type TabsProps, type TabDef } from './components/Tabs.tsx';
export { Dialog, type DialogProps } from './components/Dialog.tsx';
