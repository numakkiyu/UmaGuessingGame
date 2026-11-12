
import { test } from "node:test";
import assert from "node:assert/strict";

/** 优化前的实现（每个分位独立排序） */
function percentileOriginal(samples, p) {
  if (samples.length === 0) return null;
  const sorted = [...samples].sort((a, b) => a - b);
  const index = Math.min(sorted.length - 1, Math.floor((p / 100) * sorted.length));
  return Math.round(sorted[index]);
}

/** 优化后的实现（Float64Array 缓存一次） */
function percentileOptimized(samples, p) {
  if (samples.length === 0) return null;
  const sorted = Float64Array.from(samples).sort();
  const index = Math.min(sorted.length - 1, Math.floor((p / 100) * sorted.length));
  return Math.round(sorted[index]);
}

const CASES = [
  { name: "空数组", samples: [] },
  { name: "单元素", samples: [5] },
  { name: "两元素", samples: [1, 2] },
  { name: "五元素升序", samples: [1, 2, 3, 4, 5] },
  { name: "十元素乱序", samples: [10, 1, 5, 3, 8, 2, 9, 4, 7, 6] },
  { name: "百元素", samples: Array.from({ length: 100 }, (_, i) => i + 1) },
  { name: "千元素伪随机", samples: Array.from({ length: 1000 }, (_, i) => (i * 7919) % 1000) },
  { name: "五万元素", samples: Array.from({ length: 50000 }, (_, i) => (i * 31) % 1000) },
  { name: "含小数", samples: [1.4, 2.6, 3.5, 4.5, 5.5] },
  { name: "全部相同", samples: Array.from({ length: 100 }, () => 42) },
];

for (const testCase of CASES) {
  test(`分位数优化前后一致：${testCase.name}`, () => {
    for (const p of [50, 95, 99]) {
      const expected = percentileOriginal(testCase.samples, p);
      const actual = percentileOptimized(testCase.samples, p);
      assert.equal(actual, expected, `p${p} 不一致（n=${testCase.samples.length}）`);
    }
  });
}

test("空数组返回 null 而不是 0", () => {
  assert.equal(percentileOptimized([], 50), null);
  assert.equal(percentileOptimized([], 95), null);
  assert.equal(percentileOptimized([], 99), null);
});

test("单元素所有分位都返回该元素", () => {
  assert.equal(percentileOptimized([7], 50), 7);
  assert.equal(percentileOptimized([7], 95), 7);
  assert.equal(percentileOptimized([7], 99), 7);
});

test("分位数单调不减", () => {
  const samples = Array.from({ length: 1000 }, (_, i) => (i * 13) % 500);
  const p50 = percentileOptimized(samples, 50);
  const p95 = percentileOptimized(samples, 95);
  const p99 = percentileOptimized(samples, 99);
  assert.ok(p50 <= p95, `p50(${p50}) 应 ≤ p95(${p95})`);
  assert.ok(p95 <= p99, `p95(${p95}) 应 ≤ p99(${p99})`);
});
