/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: ['"IBM Plex Sans"', 'system-ui', '-apple-system', 'BlinkMacSystemFont', '"Segoe UI"', 'sans-serif'],
        mono: ['"IBM Plex Mono"', 'ui-monospace', 'Menlo', 'Monaco', 'monospace'],
      },
      colors: {
        // Warm graphite replaces the stock blue-tinted slate scale app-wide.
        slate: {
          50: '#f6f4f0',
          100: '#ebe8e1',
          200: '#d6d2c8',
          300: '#b7b2a5',
          400: '#a39e94',
          500: '#8c887d',
          600: '#504d45',
          700: '#3b3935',
          800: '#2a2926',
          900: '#1d1c1a',
          950: '#131211',
        },
        // The app's single accent (copper) replaces stock blue.
        blue: {
          50: '#fdf5ee',
          100: '#fbe7d4',
          200: '#f6cda8',
          300: '#efae78',
          400: '#e58f4e',
          500: '#c96a31',
          600: '#ad5527',
          700: '#8c431f',
          800: '#6f361c',
          900: '#5a2d1a',
          950: '#321709',
        },
      },
    },
  },
  plugins: [],
}
