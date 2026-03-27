import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  formatLatencyLabel,
  resolveLatencyLevel,
} from "../src/lib/realtime/latency";

describe("latency display", () => {
  it("maps low latency to green quality", () => {
    assert.equal(resolveLatencyLevel(180), "good");
    assert.equal(formatLatencyLabel(180), "正常");
  });

  it("maps mid latency to yellow quality", () => {
    assert.equal(resolveLatencyLevel(420), "ok");
    assert.equal(formatLatencyLabel(420), "良好");
    assert.equal(resolveLatencyLevel(800), "slow");
    assert.equal(formatLatencyLabel(800), "稍慢");
  });

  it("maps high latency to red quality", () => {
    assert.equal(resolveLatencyLevel(1300), "poor");
    assert.equal(formatLatencyLabel(1300), "延迟较大");
  });
});

