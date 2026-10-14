const test = require("node:test"),
  assert = require("node:assert/strict");
const { escape, minor, price } = require("../web/assets/utils.js");
test("user content is escaped", () =>
  assert.equal(
    escape('<img src=x onerror="alert(1)">'),
    "&lt;img src=x onerror=&quot;alert(1)&quot;&gt;",
  ));
test("decimal prices preserve kopecks", () => {
  assert.equal(minor("0,01"), 1);
  assert.equal(minor("123.45"), 12345);
  assert.equal(minor("0"), 0);
  for (const v of ["-1", "1e3", "1.001", "100000001"])
    assert.throws(() => minor(v));
});
test("price variants", () => {
  assert.equal(price({ price_mode: "price", price_minor: 0 }), "Бесплатно");
  assert.equal(price({ price_mode: "exchange" }), "Обмен");
});
