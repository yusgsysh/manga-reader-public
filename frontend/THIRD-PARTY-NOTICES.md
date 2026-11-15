# Third-Party Notices

## grabfold

- Source: https://github.com/javocsoft/grabfold
- npm: https://www.npmjs.com/package/grabfold
- Package: `grabfold` is a runtime dependency in `frontend/package.json`.
- Purpose: The whole "书本" reading mode — page model, gestures, fold geometry, physics and rendering.
- Usage:
  - `frontend/src/components/reader/BookFlipViewer.tsx` renders the book with the official React adapter, `GrabfoldBook` from `grabfold/react`.
  - The turning leaf is curled on the GPU by `webglFold` from `grabfold/webgl`; grabfold's own DOM fold is the fallback.
  - `frontend/src/lib/bookPages.ts` imports only the `PageRef` type from `grabfold` to map comic pages onto sheets.
  - The view is loaded lazily (`React.lazy`), so grabfold's runtime is only downloaded when the reader switches to the book mode.
- License: MIT License © JavocSoft

## EhTagTranslation/Database

- Source: https://github.com/EhTagTranslation/Database
- Purpose: Tag 中文翻译数据源（运行时通过 JSONP 从 Release 加载，不打包进源码/Bundle）
- Data: `db.text.js`（JSONP 纯文本形式，与 `db.text.json` 同源数据）
- License: 数据库文本内容除另有声明外采用 CC BY-NC-SA 3.0，并可能存在附加条款。
- 以该仓库当前 LICENSE / README 为准：https://github.com/EhTagTranslation/Database
