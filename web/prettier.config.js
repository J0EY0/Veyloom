// Formatting for the web app, set to how the code was already written: no
// semicolons, single quotes, and wide lines, since a Tailwind class list
// cannot wrap and would push a narrow limit's JSX onto a line per prop.
// `npm run format` rewrites; `npm run lint` fails on anything unformatted.

/** @type {import('prettier').Config & import('prettier-plugin-tailwindcss').PluginOptions} */
export default {
  printWidth: 160,
  semi: false,
  singleQuote: true,
  // Class lists in Tailwind's own order, which the code mostly followed
  // by hand. Tailwind 4 reads the theme from the stylesheet; cn() and
  // cva() calls are class lists too.
  plugins: ['prettier-plugin-tailwindcss'],
  tailwindStylesheet: './src/styles/theme.css',
  tailwindFunctions: ['cn', 'cva'],
  overrides: [
    {
      // CSS keeps the double quotes of Tailwind's and shadcn's own files.
      files: '*.css',
      options: { singleQuote: false },
    },
  ],
}
