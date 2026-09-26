/**
 * Run a single named validation gate.
 *
 * CI calls this instead of re-declaring the commands, so the checked-in
 * pipeline is the only definition of what each gate does. A gate that does not
 * exist fails loudly rather than quietly passing.
 *
 *   bun scripts/gate.ts test:go
 *   bun scripts/gate.ts test:go:db        # requires HIGHJACK_TEST_DATABASE_URL
 */
import { runGates, printSummary } from './lib/gates.ts';
import { validationGates } from './lib/validation-gates.ts';
import { verificationGates } from './lib/verification-gates.ts';

const requested = process.argv[2];
const searchable = [...validationGates, ...verificationGates];
const known = searchable.map((g) => g.name).join(', ');
if (!requested) {
  console.error('usage: bun scripts/gate.ts <gate-name>');
  console.error(`known gates: ${known}`);
  process.exit(2);
}

const gate = searchable.find((g) => g.name === requested);
if (!gate) {
  console.error(`unknown gate: ${requested}`);
  console.error(`known gates: ${known}`);
  process.exit(2);
}

// A mandatory gate whose precondition is unmet must fail here. Otherwise a CI
// step that cannot run its database would report success.
if (gate.skip?.() && gate.mandatory !== false) {
  console.error(`gate ${requested} is mandatory but its precondition is unmet here`);
  if (requested === 'test:go:db') {
    console.error('set HIGHJACK_TEST_DATABASE_URL to run the PostgreSQL suite');
  }
  process.exit(1);
}

const results = await runGates([gate], `gate ${requested}`);
process.exit(printSummary(results) ? 0 : 1);
