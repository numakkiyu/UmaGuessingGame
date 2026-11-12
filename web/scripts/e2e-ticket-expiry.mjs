#!/usr/bin/env node
/** Moves ticket expiry into the past in an isolated database, then checks rejection, index release and cleanup through HTTP. */
import { mkdir, writeFile } from "node:fs/promises";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const exec = promisify(execFile);
const HERE = dirname(fileURLToPath(import.meta.url));
const WEB_ROOT = resolve(HERE, "..");
const EVIDENCE = resolve(WEB_ROOT, "../.cache/reports");

const API_BASE = process.env.UMA_API_BASE ?? "http://127.0.0.1:8080";
const DB_PATH = process.env.UMA_DB_PATH ?? resolve(WEB_ROOT, "../.cache/runtime/runtime.db");

const report = {
  generatedAt: new Date().toISOString(),
  environment: { apiBase: API_BASE, dbPath: DB_PATH },
  assertions: [],
  failures: [],
  limits: [],
};

function assert(name, ok, detail) {
  const entry = { name, ok: Boolean(ok), detail };
  report.assertions.push(entry);
  if (ok) console.log(`[ticket] ✓ ${name}`);
  else {
    report.failures.push(entry);
    console.error(`[ticket] ✗ ${name}`, detail === undefined ? "" : JSON.stringify(detail));
  }
}

function note(message, detail) {
  console.log(`[ticket] · ${message}`, detail ? JSON.stringify(detail) : "");
}

/** 直接执行 SQL（模拟时间流逝、读取行数） */
async function sql(statement) {
  const { stdout } = await exec("sqlite3", [DB_PATH, statement], { maxBuffer: 8 * 1024 * 1024 });
  return stdout.trim();
}

function makeClient() {
  let cookie = "";
  let csrf = null;
  return {
    get cookie() {
      return cookie;
    },
    async call(method, path, body, idempotencyKey) {
      const headers = { Accept: "application/json, application/problem+json", Origin: API_BASE };
      if (body !== undefined) headers["Content-Type"] = "application/json";
      if (cookie) headers["Cookie"] = cookie;
      if (csrf && method !== "GET") headers["X-CSRF-Token"] = csrf;
      if (idempotencyKey) headers["Idempotency-Key"] = idempotencyKey;
      const res = await fetch(`${API_BASE}/api/v1${path}`, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      for (const c of res.headers.getSetCookie?.() ?? []) {
        const kv = c.split(";")[0];
        if (kv.startsWith("uma_session")) cookie = kv;
      }
      const text = await res.text();
      let json = null;
      try {
        json = JSON.parse(text);
      } catch {
        /* 非 JSON */
      }
      return { status: res.status, json };
    },
    async newSession(name) {
      const s = await this.call("POST", "/sessions", { displayName: name });
      csrf = s.json?.csrfToken ?? null;
      return s;
    },
  };
}

const key = () => crypto.randomUUID().replace(/-/g, "");
const CLASSIC_CFG = {
  modeId: "classic",
  maxWrongGuesses: 8,
  winPolicy: "window-attempts",
  durationSeconds: 0,
  shadowEnabled: false,
};

async function main() {
  await mkdir(EVIDENCE, { recursive: true });

  const health = await fetch(`${API_BASE}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error("[ticket] 后端未就绪");
    process.exitCode = 1;
    return;
  }

  /* ---------- 1. 建票 ---------- */
  const client = makeClient();
  await client.newSession("过期测试玩家");

  const before = await sql("SELECT COUNT(*) FROM matchmaking_tickets;");
  const rowsBefore = Number(before);

  const created = await client.call("POST", "/matchmaking-tickets", { modeConfig: CLASSIC_CFG }, key());
  assert("建票 成功", created.status === 201 && Boolean(created.json?.ticketId), {
    status: created.status,
    code: created.json?.code,
  });
  const ticketId = created.json?.ticketId;
  const sessionId = (await sql(`SELECT session_id FROM matchmaking_tickets WHERE id = '${ticketId}';`)).trim();
  note("票信息", { ticketId, sessionId, expiresAt: created.json?.expiresAt });

  /* ---------- 2. 活跃票存在时不能重复建 ---------- */
  const duplicate = await client.call("POST", "/matchmaking-tickets", { modeConfig: CLASSIC_CFG }, key());
  assert("建票 活跃票存在时被拒（partial unique index 生效）", duplicate.status === 409, {
    status: duplicate.status,
    code: duplicate.json?.code,
  });

  /* ---------- 3. 模拟时间流逝：把 expires_at 改到过去 ---------- */
  const nowMs = Date.now();
  await sql(`UPDATE matchmaking_tickets SET expires_at = ${nowMs - 60_000} WHERE id = '${ticketId}';`);
  const expiredRow = await sql(`SELECT status, expires_at FROM matchmaking_tickets WHERE id = '${ticketId}';`);
  note("已把票改成过期（1 分钟前）", { row: expiredRow, nowMs });

  /* 建票时才执行过期清理；修改到期时间后先建票，再检查旧票状态 */
  const statusBeforeTrigger = await sql(
    `SELECT status FROM matchmaking_tickets WHERE id = '${ticketId}';`,
  ).then((v) => v.trim());
  note("惰性清理前旧票状态（预期仍是 queueing）", { status: statusBeforeTrigger });

  /* ---------- 4. 关键问题 B：同一会话能否重新建票（同时触发惰性清理） ---------- */
  const reCreate = await client.call("POST", "/matchmaking-tickets", { modeConfig: CLASSIC_CFG }, key());
  assert(
    "过期 同一会话可以重新建票（唯一索引已释放）",
    reCreate.status === 201,
    {
      status: reCreate.status,
      code: reCreate.json?.code,
      detail: reCreate.json?.detail,
      note: "409 ticket-already-active 说明过期票仍占着唯一索引",
    },
  );

  /*
   * 建票之后旧票必须被标记为 expired。
   * 这是惰性清理真正生效的证据——如果没有这一步，旧票会永远占着活跃状态。
   */
  const statusAfterTrigger = await sql(
    `SELECT status FROM matchmaking_tickets WHERE id = '${ticketId}';`,
  ).then((v) => v.trim());
  assert(
    "过期 惰性清理把旧票标记为 expired（不是永远停在 queueing）",
    statusAfterTrigger === "expired",
    {
      beforeTrigger: statusBeforeTrigger,
      afterTrigger: statusAfterTrigger,
      note: "若仍是 queueing，说明清理没生效：用户关掉标签页就永久无法重新匹配",
    },
  );

  /* ---------- 5. 关键问题 C：过期票是否真的不可用 ---------- */
  if (reCreate.status === 201) {
    // 5a. 用过期票查询
    const getExpired = await client.call("GET", `/matchmaking-tickets/${ticketId}`);
    assert("过期票 查询时不可用（404 或状态为 expired）", getExpired.status === 404 || getExpired.json?.status === "expired", {
      status: getExpired.status,
      ticketStatus: getExpired.json?.status,
    });

    // 5b. 用过期票响应配对（应该被拒）
    const respondExpired = await client.call(
      "POST",
      `/matchmaking-tickets/${ticketId}/responses`,
      { accept: true },
      key(),
    );
    assert(
      "过期票 不能被用于接受配对",
      respondExpired.status >= 400,
      { status: respondExpired.status, code: respondExpired.json?.code },
    );

    // 5c. 用过期票取消（应幂等或拒绝，但不能 500）
    const cancelExpired = await client.call("DELETE", `/matchmaking-tickets/${ticketId}`, undefined, key());
    assert("过期票 取消不会 500", cancelExpired.status < 500, {
      status: cancelExpired.status,
      code: cancelExpired.json?.code,
    });
  }

  /* ---------- 5d. 伙伴过期时接受配对：必须如实报错，不能假成功 ---------- */
  {
    // An expired partner must reject acceptance without creating a room
    /*
     * 先清空残留的活跃票。
     *
     * 配对是「同模式里找最早的 queueing 票」，如果库里有前面测试留下的票，
     * A 会配到那个陌生票而不是 B —— 于是 B 保持 queueing、matched_with 为空，
     * finalisePairIfReady 在 partner 为空时直接 return nil，返回 201。
     * 这会让我们误以为「伙伴过期修复无效」，实际是测试污染。
     */
    await sql(
      "UPDATE matchmaking_tickets SET status='cancelled', updated_at=" +
        Date.now() +
        " WHERE status IN ('queueing','matched-pending-accept','ready','playing');",
    );

    const playerA = makeClient();
    await playerA.newSession("配对过期A");
    const playerB = makeClient();
    await playerB.newSession("配对过期B");

    const ticketA = await playerA.call("POST", "/matchmaking-tickets", { modeConfig: CLASSIC_CFG }, key());
    const ticketB = await playerB.call("POST", "/matchmaking-tickets", { modeConfig: CLASSIC_CFG }, key());
    note("配对建立", {
      ticketA: ticketA.json?.ticketId,
      ticketB: ticketB.json?.ticketId,
      statusA: ticketA.json?.status,
      statusB: ticketB.json?.status,
    });

    if (ticketA.status === 201 && ticketB.status === 201) {
      const pairedA = await playerA.call("GET", `/matchmaking-tickets/${ticketA.json.ticketId}`);
      const pairedB = await playerB.call("GET", `/matchmaking-tickets/${ticketB.json.ticketId}`);
      /*
       * 必须确认这两张票互相配对，而不是 A 配到了别的票。
       * 若 B 仍是 queueing，说明配对对象不是 B，后续「伙伴过期」测的就不是这个场景。
       */
      assert(
        "配对 A、B 两张票互相配对（不是配到陌生票）",
        pairedA.json?.status === "matched-pending-accept" && pairedB.json?.status === "matched-pending-accept",
        { statusA: pairedA.json?.status, statusB: pairedB.json?.status },
      );

      const roomsBefore = Number(await sql("SELECT COUNT(*) FROM rooms;"));

      // 把 A 的票改到过期
      const nowMs2 = Date.now();
      await sql(
        `UPDATE matchmaking_tickets SET expires_at = ${nowMs2 - 120_000} WHERE id = '${ticketA.json.ticketId}';`,
      );

      // B 接受配对
      const acceptB = await playerB.call(
        "POST",
        `/matchmaking-tickets/${ticketB.json.ticketId}/responses`,
        { accept: true },
        key(),
      );
      const roomsAfter = Number(await sql("SELECT COUNT(*) FROM rooms;"));
      const statusBAfter = await sql(
        `SELECT status FROM matchmaking_tickets WHERE id = '${ticketB.json.ticketId}';`,
      ).then((v) => v.trim());
      const statusAAfter = await sql(
        `SELECT status FROM matchmaking_tickets WHERE id = '${ticketA.json.ticketId}';`,
      ).then((v) => v.trim());

      note("伙伴过期后接受的结果", {
        acceptStatus: acceptB.status,
        code: acceptB.json?.code,
        roomsBefore,
        roomsAfter,
        statusA: statusAAfter,
        statusB: statusBAfter,
      });

      // 关键断言 1：不得创建房间（副作用没发生）
      assert("伙伴过期 接受配对不得创建房间", roomsAfter === roomsBefore, {
        roomsBefore,
        roomsAfter,
      });

      // A successful status would misrepresent an uncompleted match
      assert(
        "伙伴过期 接受配对必须返回明确失败（不能 2xx 假成功）",
        acceptB.status >= 400,
        {
          status: acceptB.status,
          code: acceptB.json?.code,
          note: "2xx 意味着玩家以为匹配成功，实际永远等不到房间",
        },
      );

      // 关键断言 3：接受方的票不应卡在 ready
      assert(
        "伙伴过期 接受方的票不应停留在 ready（否则玩家永远等房间）",
        statusBAfter !== "ready",
        { statusBAfter, note: "ready 表示'我已接受，等对方'，但对方已过期永远不会 ready" },
      );
    } else {
      report.limits.push("伙伴过期场景跳过：配对建立失败");
    }
  }

  /* ---------- 6. 行数增长：清理是否真的删除行 ---------- */
  {
    const after = Number(await sql("SELECT COUNT(*) FROM matchmaking_tickets;"));
    const expiredCount = Number(
      await sql("SELECT COUNT(*) FROM matchmaking_tickets WHERE status = 'expired';"),
    );
    const activeCount = Number(
      await sql(
        "SELECT COUNT(*) FROM matchmaking_tickets WHERE status IN ('queueing','matched-pending-accept','ready','playing');",
      ),
    );
    note("票表统计", { rowsBefore, rowsAfter: after, expiredCount, activeCount });

    /* 活跃票唯一性按 session_id 约束，不限制不同会话的全局票数 */
    const duplicatedSessions = Number(
      await sql(
        "SELECT COUNT(*) FROM (SELECT session_id FROM matchmaking_tickets " +
          "WHERE status IN ('queueing','matched-pending-accept','ready','playing') " +
          "GROUP BY session_id HAVING COUNT(*) > 1);",
      ),
    );
    assert(
      "清理 同一会话不会持有多个活跃票（partial unique index 生效）",
      duplicatedSessions === 0,
      {
        duplicatedSessions,
        activeCount,
        note: "全局活跃票数 >1 是正常的（多个会话各一张），约束的是每会话唯一",
      },
    );

    /*
     * 关键问题 C：清理是惰性的还是定时任务？
     *
     * 如果只是查询时过滤（不改 status、不删行），过期行会无限累积。
     * 这里只断言「不会无限增长」这个方向——单次测试无法证明长期行为，
     * 但能记录当前实现的事实。
     */
    const totalRows = after - rowsBefore;
    report.limits.push(
      `票行数：本轮新增 ${totalRows} 行（${rowsBefore} → ${after}），` +
        `其中 expired=${expiredCount}、active=${activeCount}。` +
        "单次运行无法判定长期是否会无限累积，需要定时任务或周期性清理才能保证；" +
        "当前实现若不删除过期行，行数会随使用线性增长。",
    );
  }

  /* ---------- 7. 服务端无 5xx ---------- */
  {
    const logPath = process.env.UMA_SERVER_LOG ?? "/tmp/uma-server.err";
    try {
      const { stdout } = await exec("bash", ["-c", `tail -n 200 ${logPath} 2>/dev/null | grep -cE '"status":5[0-9][0-9]' || true`]);
      const count5xx = Number(stdout.trim() || 0);
      assert("票流程 未产生 5xx", count5xx === 0, { count5xx, window: "最近 200 行日志" });
    } catch {
      report.limits.push("5xx 检查跳过：读不到服务端日志");
    }
  }

  report.summary = {
    assertions: report.assertions.length,
    passed: report.assertions.filter((a) => a.ok).length,
    failed: report.failures.length,
  };

  const out = resolve(EVIDENCE, "ticket-expiry-e2e.json");
  await writeFile(out, `${JSON.stringify(report, null, 2)}\n`, "utf8");
  console.log(
    `\n[ticket] ${report.summary.passed}/${report.summary.assertions} 断言通过，${report.summary.failed} 失败`,
  );
  console.log(`[ticket] 报告：${out}`);
  if (report.failures.length > 0) process.exitCode = 1;
}

await main();
