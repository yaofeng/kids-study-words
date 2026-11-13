# 第三方声明

本项目在构建和运行时使用以下第三方数据与软件。感谢这些项目及其贡献者。

## 数据

### ECDICT

- 用途：离线英汉词典，构建 `dict.db` 的词条、音标、释义和词形变化。
- 来源：<https://github.com/skywind3000/ECDICT>
- 许可证：MIT
- 本项目固定使用提交 `9c46923fb81a2eb6c264a3a1689562ea7bc56b6a` 的 `ecdict.csv`，
  构建时校验 SHA-256 `d1ecbdf9ee2b920afb6f430ca6b6bb9f559588cf0dff86e04bd255f9390843cc`。

### Tatoeba 中英对照句对

- 用途：查词时的本地例句（构建 `dict.db` 的 `sentences` 表）。
- 来源：Tatoeba 语料经 <https://www.manythings.org/anki/> 整理的 `cmn-eng.zip`。
- 许可证：CC-BY 2.0 FR（句子署名 Tatoeba 贡献者）。
- 构建时校验 SHA-256 `d606361aa7312dcdfb15ef4fc51cb1e7ed8bc2dc608a7b192626cd36e8403106`。

## 软件

### htmx

- 用途：前端局部刷新。
- 来源：<https://htmx.org/>
- 许可证：BSD 2-Clause

### Tailwind CSS

- 用途：界面样式；发布产物为 `web/static/app.css`。
- 来源：<https://tailwindcss.com/>
- 许可证：MIT

### Nunito

- 用途：界面字体；字体文件位于 `web/static/fonts/`。
- 来源：<https://fonts.google.com/specimen/Nunito>
- 许可证：SIL Open Font License 1.1（见 `web/static/fonts/OFL.txt`）
