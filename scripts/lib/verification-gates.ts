/**
 * The single definition of the HighJack verification gate list.
 *
 * Verification is the behavioural half of certification: it proves the built
 * artifact behaves as intended for users, against the real backend, rather
 * than merely compiling. Kept in its own module so `validate` (static +
 * unit gates only) stays fast while `certify` and CI share one definition.
 */
import type { Gate } from './gates.ts';

export const verificationGates: Gate[] = [
  // Runs against the production builds produced by build:web / build:game, and
  // the Playwright config boots the real Go server, so this is a genuine
  // end-to-end check rather than a mock.
  //
  // There is deliberately no skip hook. Browser verification used to be
  // skippable with HIGHJACK_SKIP_BROWSER=1 while `certify` still exited 0,
  // which let a release pipeline certify a build with no behavioural
  // verification at all (audit OPS-9). If this gate cannot run, certification
  // must not pass.
  {
    name: 'verify:browser',
    cmd: ['bunx', 'playwright', 'test'],
    cwd: 'apps/web',
  },
];

/** Every gate known to the pipeline, validation and verification together. */
export const allGates = (validation: Gate[]): Gate[] => [...validation, ...verificationGates];
