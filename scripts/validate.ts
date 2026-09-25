/**
 * HighJack unified validation pipeline.
 *
 * Runs every mandatory gate in dependency order and fails fast.
 * See docs/development/VALIDATION.md for the gate reference.
 */
import { runGates, printSummary } from './lib/gates.ts';
import { validationGates } from './lib/validation-gates.ts';

const results = await runGates(validationGates, 'validation');
process.exit(printSummary(results) ? 0 : 1);
