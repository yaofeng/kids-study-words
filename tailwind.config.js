/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./web/templates/**/*.html', './web/static/app.js'],
  theme: {
    extend: {
      colors: {
        cream: '#FFF8EE',
        paper: '#FFFFFF',
        ink: '#3B3A36',
        muted: '#7A756C',
        line: '#3B3A36',
        teal: '#4FB3A9',
        'teal-dark': '#237A72',
        coral: '#FF9F80',
        'coral-dark': '#D9633F',
        sun: '#FFD966',
        lilac: '#B9A7E6',
      },
      fontFamily: {
        rounded: ['Nunito', 'system-ui', '-apple-system', 'PingFang SC', 'Microsoft YaHei', 'sans-serif'],
      },
      boxShadow: {
        sticker: '3px 3px 0 #3B3A36',
      },
      borderRadius: {
        blob: '20px',
      },
    },
  },
  plugins: [],
};
