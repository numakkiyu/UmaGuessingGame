# API 與即時通訊

[OpenAPI](https://github.com/numakkiyu/UmaGuessingGame/blob/main/api/openapi.json) 定義 HTTP 操作、欄位、錯誤與寫入要求，[即時契約](https://github.com/numakkiyu/UmaGuessingGame/blob/main/api/realtime.schema.json) 定義通知

## 基本約定

前綴為 `/api/v1`，JSON 欄位使用 camelCase，ID 視為不透明字串。錯誤為 RFC 9457 的 `application/problem+json`，前端依穩定類型處理，不顯示底層堆疊

一般已認證寫入按操作提供 Cookie、Origin、`X-CSRF-Token`、`Idempotency-Key`，需要時附階段與狀態版本。建立身分與回呼等介面有各自條件

## 呼叫順序

1. 讀公開設定、玩法與角色資料
2. 建立或恢復匿名會話並取得 CSRF
3. 按設定完成入場確認，建房或加入
4. 讀取有權限的房間與回合狀態
5. REST 提交動作，儲存原指令鍵與版本
6. 訂閱變更並以游標恢復，最後讀取正式成績

## 重試

同一邏輯指令重用原鍵與請求內容，新操作使用新鍵。逾時時讀回執或狀態，不直接產生第二次作答。版本衝突先同步，限流遵循退避。取消前端請求不撤銷已提交交易

## 即時通知

預設 SSE，可選 WebSocket，失敗後游標輪詢。SSE 使用命名事件，須逐項監聽。即時通道只提供通知，動作仍走 REST

重新連線先關閉舊通道。epoch 變更、游標過期或要求恢復時重新讀取狀態。代理對 SSE 停用緩衝，對 WebSocket 轉發 Upgrade

## 配對介面

以下路徑均加 `/api/v1`

| 方法與路徑 | 用途 | operationId |
| --- | --- | --- |
| GET /matchmaking-lobby | 開放佇列與人數統計 | `getMatchmakingLobby` |
| POST /matchmaking-proofs | 提交 poolId，取得 90 秒工作量題 | `createMatchmakingProof` |
| GET /matchmaking-tickets/current | 目前票據，沒有時 ticket 為 null | `getActiveMatchmakingTicket` |
| POST /matchmaking-tickets | 提交 poolId、proof.challengeId、proof.nonce | `createMatchmakingTicket` |
| POST /matchmaking-tickets/{ticketId}/heartbeat | 更新在場租約 | `heartbeatMatchmakingTicket` |
| POST /matchmaking-tickets/{ticketId}/responses | 接受或拒絕配對 | `respondToMatch` |
| DELETE /matchmaking-tickets/{ticketId} | 取消尚未開局的票據 | `cancelMatchmakingTicket` |

工作量為 UTF-8 的 `challengeId + ":" + nonce` 的 SHA-256 前導零位元數，nonce 為不超過 16 位的十進位無號整數文字

狀態依 `queueing`、`matched-pending-accept`、`ready`、`playing` 推進。ready 不保證已有 roomId，取得房間後才導向。結束狀態為 cancelled、expired、finished

建立、回應與取消須保留原冪等鍵。票據、房間與回合分配在 SQLite 交易內完成。隨機房只接受已配對成員，不提供邀請碼與管理權

## 期限與結果

`received_at` 為伺服器入隊時間，決定截止與先後，不接受裝置時鐘。空閒到期後首筆作答返回 `409 matchmaking-idle`，已接受操作仍返回原回執

`RoundView.canSubmit` 受資格、代表、暫停、階段、期限、投降與玩法限制。伺服器寫入時重驗。投降後新作答為 participant-surrendered，截止後為 deadline-passed

正式成績讀 `results[].scoreUnits`，以 subjectId 和揭曉／結算階段對應，0 為有效分數。outcome 為 won、lost、draw、completed、surrendered、disconnected、void

## 權限與保護

投影依觀看者與階段，答案、私有線索、未公開投票不提前下發。Service Worker 不快取認證 API 或私有狀態

退出、到期、撤銷與換裝置會失效舊連線。席位恢復在同一交易消耗恢復碼、綁定原成員、轉移管理權，再發新 Cookie，原進度保留

請求有地址、會話、動作與資源額度；實時握手、總連線、單房與單會話分開限制。WebSocket 僅接受有限大小與頻率的心跳／游標確認，SSE 釋放慢寫入者。CSP 限制腳本來源，權限仍由伺服器驗證

## 開局條件與操作權限

`Mode.startRule` 區分 standard、clue-puzzle、rapport、trade、strategy-race。前端依人數與隊伍條件提供提示，伺服器於開局交易再次檢查

換卡提案與回應均確認雙方資格；對已投降者的未成交提案返回 `trade-closed`。暫停須由另一計分方回應，同隊回應為 `pause-counterparty-required`，已回應為 `pause-request-closed`，階段變更為 `pause-request-stale`

寫入交易重新確認成員、隊伍、會話綁定與有效期。身分變更後拒絕舊請求，不自動換另一身分重送。通知通道預設每 30 秒複核，退出主動撤銷，席位恢復只撤銷原席位舊會話。outbox 的撤權欄位僅供服務端，不進入客戶端重播

CSP 限制本站及選用 Turnstile 的腳本來源，禁止內嵌腳本、eval、物件與外站框架嵌入。F12、視窗尺寸、瀏覽器指紋和擴充功能不作自動判負依據；操作合法性、Origin／CSRF、階段及限流由伺服器檢查

## 入場確認

GET `/api/v1/sessions/current/admission` 返回 enabled、required、verified，不建立身分。POST 同路徑需會話、Origin 與 CSRF，後端使用 Cloudflare Siteverify 驗證網域、動作與時間

admission-required 是普通確認，admission-risk-required 是自身操作持續限流後的重新確認，均為 403。確定拒絕的原請求最多恢復一次，不循環重送。權杖摘要按會話短時占用與消費，不儲存原文

## 帳號與計畫介面

[帳號 API](账户登录与游玩准入.md)是已實作、預設停用的實驗性功能，Cookie 與 CSRF 和匿名遊玩分開。公開提供方只含 ID、可用性、名稱與圖示

帳號計分挑戰與榜單尚未註冊。計畫 `/scored-challenges` 與 `/leaderboards/{boardId}/entries`、`/me`，不與既有匿名 `/challenges` 混用

## 更新契約

```bash
make web-api-types
pnpm --dir web run api:check
go run ./cmd/content contract --spec api/openapi.json --routes
```

型別產生於 `web/src/lib/api/schema.d.ts`，修改契約後重新產生，不手動改寫
