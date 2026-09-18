import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

// A Tailwind size utility with a px arbitrary value, e.g. text-[13px],
// max-w-[820px], calc(100%-24px) inside one.
const pxSize =
  '/(^|[\\s:"\'`])-?(text|leading|tracking|size|w|h|min-w|max-w|min-h|max-h|basis|gap|gap-x|gap-y|p|px|py|pt|pr|pb|pl|m|mx|my|mt|mr|mb|ml|top|right|bottom|left|inset|inset-x|inset-y)-\\[[^\\]]*\\dpx[^\\]]*\\]/'

export default defineConfig([
  globalIgnores(['dist', 'node_modules', 'test-results', 'playwright-report']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [js.configs.recommended, tseslint.configs.recommended, reactHooks.configs.flat.recommended],
    languageOptions: { ecmaVersion: 2022, globals: globals.browser },
    rules: {
      // A file that grows past this is two things pretending to be one:
      // split it before the rule has to fail.
      'max-lines': ['error', { max: 300, skipBlankLines: true, skipComments: true }],
    },
  },
  {
    // Sizes are rem so the interface size setting (src/lib/uiSize.ts)
    // scales text and spacing together; a px size in a class name would
    // stay put while everything around it grows. Radii, rings and borders
    // are hairline detail and keep their px.
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/components/ui/**', 'src/components/ai-elements/**'],
    rules: {
      'no-restricted-syntax': [
        'error',
        ...['Literal[value=' + pxSize + ']', 'TemplateElement[value.raw=' + pxSize + ']'].map((selector) => ({
          selector,
          message: 'Use rem (or the Tailwind scale) for sizes, not px: the interface size setting scales rem.',
        })),
      ],
    },
  },
  {
    // The string catalogues are one table each; splitting them by page
    // would only scatter the keys.
    files: ['src/i18n/**'],
    rules: { 'max-lines': 'off' },
  },
  {
    // Vendored by `shadcn add` (shadcn/ui and AI Elements): kept as
    // published so the next `add --overwrite` is a clean diff.
    files: ['src/components/ui/**', 'src/components/ai-elements/**', 'src/hooks/use-mobile.ts'],
    linterOptions: { reportUnusedDisableDirectives: 'off' },
    rules: {
      '@typescript-eslint/no-unused-vars': 'off',
      'react-hooks/exhaustive-deps': 'off',
      'max-lines': 'off',
      'react-hooks/refs': 'off',
      'react-hooks/set-state-in-effect': 'off',
      'react-hooks/immutability': 'off',
      'react-hooks/purity': 'off',
      'react-hooks/static-components': 'off',
      '@typescript-eslint/no-explicit-any': 'off',
      '@typescript-eslint/no-empty-object-type': 'off',
      'no-useless-assignment': 'off',
    },
  },
])
