// @ts-check
import tseslint from 'typescript-eslint';

/**
 * HighJack TypeScript lint rules.
 *
 * Scope: correctness and boundary hygiene, not style (formatting belongs to
 * Prettier). Type-aware linting is intentionally not enabled at this scale;
 * strict tsc covers the type-safety floor.
 */
export default tseslint.config(
  {
    ignores: [
      '**/dist/**',
      '**/.astro/**',
      '**/node_modules/**',
      'verification/**',
      '**/test-results/**',
      '**/playwright-report/**',
    ],
  },
  ...tseslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx,mts}'],
    rules: {
      // Dependency policy: `any` must be explicit and justified in review.
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/no-unused-vars': [
        'error',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_' },
      ],
      'no-console': 'off',
      eqeqeq: ['error', 'always'],
    },
  },
);
