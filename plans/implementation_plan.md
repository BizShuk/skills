# 實作計畫 (Implementation Plan) - 修正插件分類與 TUI 滑動窗口分頁

## 問題分析與描述 (Problem Analysis & Description)

當執行 `skills add .` 來安裝包含多個插件的 `Claude` Code 插件時，遇到以下兩個主要問題：

1. `插件分類與常規技能搜尋問題 (Plugin Categories & Conventional Skill Discovery)`：
   - 插件明細 `marketplace.json` 或 `plugin.json` 中的 `skills` 列表可能未宣告所有技能，或者技能被置於目錄層級例如 `skills/<category>/<skill>/SKILL.md`（插件分類結構）。
   - 程式碼 `src/skills.ts` 中，`deepContainerDirs` 是在 `prioritySearchDirs.push(...getPluginSkillPaths)` 之前被定義的，使得插件宣告的搜尋路徑無法進行深度搜尋。
   - 程式碼 `src/plugin-manifest.ts` 的 `getPluginGroupings` 僅在 `manifest.skills` 明確列出時才設定群組對照。常規方式擺放的技能（Conventional skills）無法映射到其對應的插件，導致其在 TUI 介面上被歸類到 `Other` 分組。

2. `TUI 選擇畫面溢出損壞 (TUI Selection Screen Overflow & Broken)`：
   - 當插件與技能數量過多時，原先使用的 `@clack/prompts` 的 `groupMultiselect` 與 `multiselect` 會一次性把所有選項印在終端機上，導致選擇介面溢出螢幕，使用者無法正常看清選取的項目。
   - 原先的 `searchMultiselect` 雖有滑動窗口 (sliding window) 與過濾搜尋功能，但不支持分組 (grouping/categories)。

## 變更計畫 (Proposed Changes)

### 1. 核心邏輯變更 (Core Logic Changes)

#### [MODIFY] [plugin-manifest.ts](file:///Users/shuk/projects/tmp/skills/src/plugin-manifest.ts)
- 在 `getPluginGroupings` 中，除了掃描 `manifest.skills` 定義的技能外，額外掃描每個插件的常規 `skills/` 目錄。
- 同時支援 `skills/<skill-name>/SKILL.md` 與含有分類的 `skills/<category>/<skill-name>/SKILL.md` 結構，自動將它們與其所屬插件進行 `pluginName` 的映射。

#### [MODIFY] [skills.ts](file:///Users/shuk/projects/tmp/skills/src/skills.ts)
- 修改 `discoverSkills` 邏輯，確保所有以 `/skills` 或 `\skills` 結尾的插件搜尋目錄均加入 `deepContainerDirs`，允許這些常規插件目錄進行一級深度的遞迴搜尋，以便發現 `skills/<category>/<skill-name>/SKILL.md` 目錄結構。

### 2. TUI 與互動介面變更 (TUI & Interactive Prompts Changes)

#### [MODIFY] [search-multiselect.ts](file:///Users/shuk/projects/tmp/skills/src/prompts/search-multiselect.ts)
- 擴充 `SearchItem` 結構體，新增可選的 `group` 欄位。
- 在 `searchMultiselect` 中，當使用者輸入過濾關鍵字時，亦可對 `item.group` 進行模糊搜尋。
- 在畫面渲染階段：
  - 基於當前過濾後且落在 `滑動窗口 (sliding window)` 內的可見項目進行分組渲染。
  - 當分組名 `group` 改變時，動態渲染分組標題（如 `Category Name`）。
  - 對屬於某個分組的技能項目進行縮排 (indentation) 顯示，確保視覺分級清晰。
  - 保留固定的滑動窗口大小限制（如最大 `maxVisible = 12`），確保選項再多也不會溢出終端機畫面。

#### [MODIFY] [add.ts](file:///Users/shuk/projects/tmp/skills/src/add.ts)
- 在技能選擇環節，以自訂的 `searchMultiselect` 取代原先的 `@clack/prompts` `groupMultiselect` 與 `multiselect`。
- 自動將插件名稱 `pluginName` 映射至 `group`，讓技能在安裝時具備分組搜尋滑動窗口與分頁。

#### [MODIFY] [remove.ts](file:///Users/shuk/projects/tmp/skills/src/remove.ts)
- 在技能移除環節，以自訂的 `searchMultiselect` 取代原先的 `p.multiselect`，以避免已安裝技能過多時所造成的 TUI 溢出問題。

---

## 驗證計畫 (Verification Plan)

### 自動化測試 (Automated Tests)
- 執行 `pnpm test` 來驗證現有測試的相容性。
- 新增/修改測試以驗證：
  1. `getPluginGroupings` 能夠正確映射常規/分類路徑的技能。
  2. `discoverSkills` 能夠正確走訪具有分類目錄的常規技能路徑。

### 手動驗證 (Manual Verification)
- 執行 `pnpm dev add /Users/shuk/projects/cc-plugin`。
- 確認當前終端機顯示的技能選擇選單：
  1. 呈現分頁滑動窗口（最多顯示 12 個選項 + 上下箭頭數量提示）。
  2. 選項依插件分組呈現且有適當的縮排。
  3. 可以輸入關鍵字對技能名稱、說明 or 插件組別進行即時過濾。
