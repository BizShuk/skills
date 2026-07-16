# 對齊 ccstatusline Token 計算設計

## 目標

讓本 repo 的 Claude token 統計與 `ccstatusline` 使用一致的欄位語意與 streaming 去重規則，同時保留 Codex 與 Antigravity 的既有資料來源。

## 計算規則

- `InputTokens` 只包含 `input_tokens`。
- `CacheReadTokens` 包含 `cache_read_input_tokens` 或 Codex 的 `cached_input_tokens`。
- `CacheCreationTokens` 包含 `cache_creation_input_tokens`。
- `OutputTokens` 包含 `output_tokens`。
- Total = Input + Cache Read + Cache Creation + Output。
- Claude 同一 `requestId` 只取 final assistant entry；尚未完成時取該 request 的最新 entry。
- 沒有 `requestId` 的舊格式維持逐筆計算，避免錯誤合併獨立請求。

## Cache 相容性

日統計 cache 增加 schema version。舊 cache 的 `InputTokens` 已混入 cache，無法還原，因此讀到舊版本時回傳錯誤並重新解析。

## 報告

總覽與每日表格新增 `Cached` 欄位；`Input` 不再包含 cache。Total 顯示四類 token 的總和。

## 不在本次範圍

Antigravity 仍使用現有的每 3 字元估算方式，因 `ccstatusline` 只處理 Claude Code transcript。
