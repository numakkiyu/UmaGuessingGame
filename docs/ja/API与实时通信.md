# API とリアルタイム通信

[OpenAPI](https://github.com/numakkiyu/UmaGuessingGame/blob/main/api/openapi.json) が HTTP 操作、項目、エラー、書き込み条件を定義し、[通知スキーマ](https://github.com/numakkiyu/UmaGuessingGame/blob/main/api/realtime.schema.json) がリアルタイム通知を定義します

## 基本形式

プレフィックスは `/api/v1`、JSON は camelCase、ID は不透明な文字列です。エラーは RFC 9457 の `application/problem+json` を返し、画面では安定した型ごとに案内します

通常の認証済み書き込みには Cookie、Origin、`X-CSRF-Token`、`Idempotency-Key`、必要に応じて段階と状態のバージョンを渡します。セッション作成やコールバックは操作別の条件に従います

## 呼び出し順序

1. 公開設定、種目、キャラクター情報を取得
2. 匿名セッションを作成・復元し CSRF を取得
3. 設定に応じて入場確認を済ませ、ルーム作成・参加
4. 権限のあるルームとラウンド状態を取得
5. REST で操作を送り、元のコマンドキーとバージョンを保持
6. 変更通知を購読し、カーソルで復元、終了時に正式成績を取得

## 再送

同じ論理コマンドは元のキーと本文、新しい操作は新しいキーを使います。タイムアウト時は受領記録か状態を取得し、二つ目の回答を生成しません。競合時は状態を更新し、頻度制限では待機します。ブラウザー側の中止でコミット済みの処理は戻りません

## 通知

既定は SSE、任意で WebSocket、失敗後はカーソル付きポーリングです。SSE は名前付きイベントを購読し、操作自体は REST で送ります

再接続では旧接続を閉じます。epoch 変更、カーソル失効、復元要求では状態を取り直します。プロキシは SSE をバッファリングせず、WebSocket Upgrade を転送します

## マッチング API

以下に `/api/v1` を付けます

| メソッド・パス | 用途 | operationId |
| --- | --- | --- |
| GET /matchmaking-lobby | 公開キューと人数集計 | `getMatchmakingLobby` |
| POST /matchmaking-proofs | poolId から 90 秒の作業量問題を取得 | `createMatchmakingProof` |
| GET /matchmaking-tickets/current | 現在のチケット。なければ ticket: null | `getActiveMatchmakingTicket` |
| POST /matchmaking-tickets | poolId、proof.challengeId、proof.nonce を送信 | `createMatchmakingTicket` |
| POST /matchmaking-tickets/{ticketId}/heartbeat | 在席リース更新 | `heartbeatMatchmakingTicket` |
| POST /matchmaking-tickets/{ticketId}/responses | 参加または辞退 | `respondToMatch` |
| DELETE /matchmaking-tickets/{ticketId} | 対戦開始前の取消 | `cancelMatchmakingTicket` |

作業量は UTF-8 の `challengeId + ":" + nonce` の SHA-256 上位ゼロビット数です。nonce は最大 16 桁の符号なし十進整数文字列です

状態は queueing、matched-pending-accept、ready、playing。ready だけではルームがあるとは限らず、roomId を取得してから移動します。終了は cancelled、expired、finished です

作成・回答・取消は元の冪等キーを保持します。チケット、ルーム、ラウンドの割当は SQLite の同一トランザクションです。ランダムルームは割当済み参加者だけが入り、招待コードや管理権限は使いません

## 締め切りと結果

`received_at` はサーバーが入隊時に付与する時刻で、締め切りと順序に使います。端末時刻は受け付けません。無操作期限後の最初の回答は `409 matchmaking-idle`、受領済み操作は元の記録を返します

`RoundView.canSubmit` は参加資格、代表者、停止、段階、期限、リタイア、種目の制約で決まり、書き込み時にも検証します。リタイア後の新規操作は participant-surrendered、締め切り後は deadline-passed です

正式得点は `results[].scoreUnits`。subjectId と公開・精算段階に対応させ、0 点も有効です。outcome は won、lost、draw、completed、surrendered、disconnected、void です

## 権限と保護

答え、非公開ヒント、未公開投票は閲覧者と段階に応じて投影し、先に配信しません。Service Worker は認証 API と非公開状態をキャッシュしません

退出、期限、失効、端末変更では旧接続を無効にします。参加枠の復元は復元コードの消費、元メンバーの再紐付け、管理権移転を同じトランザクションで行ってから Cookie を発行し、回答は保持します

接続元・セッション・操作・資源別に頻度制限し、ハンドシェイク、全体、ルーム、セッションの接続上限を分けます。WebSocket は制限付きの heartbeat とカーソル確認、SSE は遅い書き込みを切断します。CSP とサーバーの権限検証を併用します

## 開始条件と操作権限

`Mode.startRule` は standard、clue-puzzle、rapport、trade、strategy-race を区別します。人数とチームの条件を画面で案内し、サーバーは開始トランザクションで再確認します

カード交換は双方の参加資格を確認し、降参した相手への未成立提案には `trade-closed` を返します。一時停止への応答は別の採点主体に限り、同じチームなら `pause-counterparty-required`、応答済みなら `pause-request-closed`、段階変更後なら `pause-request-stale` です

書き込みトランザクションで参加者、チーム、セッションの所属と期限を再確認します。身元変更後の旧リクエストを拒否し、別の身元に切り替えて自動再送しません。通知接続は既定で 30 秒ごとに確認し、退出では直ちに失効します。参加枠の復元では元の枠の旧セッションだけを失効させます。outbox の失効情報はサーバー内で使い、クライアントの再送データに含めません

CSP は自サイトと任意の Turnstile のスクリプトに制限し、インラインスクリプト・eval・オブジェクト埋め込み・外部フレームへの埋め込みを禁止します。F12、画面サイズ、指紋、拡張機能では自動的に敗北扱いにしません。操作、Origin／CSRF、段階、頻度制限はサーバーで確認します

## 入場確認

GET `/api/v1/sessions/current/admission` は enabled、required、verified を返し、セッションを作りません。POST はセッション、Origin、CSRF が必要で、Cloudflare Siteverify によりドメイン、操作、時刻を検証します

admission-required は通常確認、admission-risk-required は自身の操作が継続して制限された後の再確認で、どちらも 403 です。明確に拒否された元の操作だけ一回復元し、再送ループは作りません。トークン原文は保存せず、セッション別の短期ハッシュ状態を管理します

## アカウントと予定 API

[アカウント API](账户登录与游玩准入.md) は実装済みの実験的機能で、既定では無効です。Cookie と CSRF は匿名プレイと分離します。公開提供元には ID、可用性、表示名、アイコンだけを含めます

アカウント用の採点とランキングはルート未登録です。予定は `/scored-challenges`、`/leaderboards/{boardId}/entries`、`/me` で、既存の匿名 `/challenges` とは別です

## 契約の更新

```bash
make web-api-types
pnpm --dir web run api:check
go run ./cmd/content contract --spec api/openapi.json --routes
```

`web/src/lib/api/schema.d.ts` は契約から再生成し、直接編集しません
