const path = require('path');
const createFontSlice = require('font-slice');

createFontSlice({
  // 请将 HarmonyOS_Sans_SC_Regular.ttf 放到本文件同级的 fonts/ 目录下
  fontPath: path.resolve(__dirname, 'fonts/HarmonyOS_Sans_SC_Regular.ttf'),
  outputDir: path.resolve(__dirname, '../../backend/web/fonts/'),
  formats: ['woff2'],
  fontFamily: 'HarmonyOS Sans SC',
  fontDisplay: 'swap',
  preview: false,
});
