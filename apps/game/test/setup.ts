import '@testing-library/jest-dom/vitest';
import { cleanup } from '@solidjs/testing-library';
import { afterEach } from 'vitest';

// Component tests render into a fresh document per file. Without cleanup,
// several cases in one file would share mounted trees and a query could match
// a button from a previous case — the same class of leak the store reset
// prevents for signals.
afterEach(() => {
  cleanup();
});
