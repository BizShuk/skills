# 術語表 (Terminology)

本文件是 `skills` 專案領域名詞、欄位與狀態值的單一定義來源。

| 術語 | 定義 |
| --- | --- |
| 全域規則 (global rule) | 安裝到 agent user-level 規則路徑的 Markdown 指令文件。`skills install [url]` 下載一次來源內容，再寫入每個選定 agent 的目標路徑。 |
| `globalRulePath` | `svc/agent/providers/*.json` 的 provider 欄位，定義該 agent 的全域規則檔路徑；`~/` 會在建立 `agent.Agent` 時展開為目前使用者的 home 目錄。 |
