# Third-Party Notices

## grabfold

- Source: https://github.com/javocsoft/grabfold
- npm: https://www.npmjs.com/package/grabfold
- Package: `grabfold` is a runtime dependency in `frontend/package.json`.
- Purpose: Page-fold geometry, physics and rendering for the "书本翻页" animation.
- Usage: The reader imports `foldGeometry`, `foldDepth`, `releaseSpeed`, `releaseTarget`, `settleDuration`, `peekProgress`, `FoldingSheet` and `gutterGradient` directly from `grabfold` via `frontend/src/lib/bookFold.ts`. The `BookFoldRenderer` overlays a grabfold `FoldingSheet` on comimi's stage during a book turn.
- License: MIT License © JavocSoft

## EhTagTranslation/Database

- Source: https://github.com/EhTagTranslation/Database
- Purpose: Tag 中文翻译数据源（运行时通过 JSONP 从 Release 加载，不打包进源码/Bundle）
- Data: `db.text.js`（JSONP 纯文本形式，与 `db.text.json` 同源数据）
- License: 数据库文本内容除另有声明外采用 CC BY-NC-SA 3.0，并可能存在附加条款。
- 以该仓库当前 LICENSE / README 为准：https://github.com/EhTagTranslation/Database
