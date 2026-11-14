/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./web/templates/**/*.html', './web/static/app.js'],
  theme: {
    extend: {
      colors: {
        sky: '#F2FAFF',
        paper: '#FFFFFF',
        ink: '#1E2D42',
        muted: '#5B6B80',
        line: '#1E2D42',
        blue: '#3D9BF0',
        'blue-deep': '#1F73D6',
        'blue-text': '#15529E',
        'blue-soft': '#A9DCFF',
        sun: '#FFD966',
        mint: '#66D9C7',
        coral: '#FF8A73',
        'coral-dark': '#A93A22',
        lilac: '#B9A7E6',
      },
      fontFamily: {
        rounded: ['Nunito', 'system-ui', '-apple-system', 'PingFang SC', 'Microsoft YaHei', 'sans-serif'],
      },
      borderRadius: {
        blob: '20px',
      },
    },
  },
  plugins: [],
};
