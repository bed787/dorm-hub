"use strict";
(function (root) {
  const escape = (value) =>
    String(value ?? "").replace(
      /[&<>"']/g,
      (c) =>
        ({
          "&": "&amp;",
          "<": "&lt;",
          ">": "&gt;",
          '"': "&quot;",
          "'": "&#39;",
        })[c],
    );
  function minor(value) {
    const text = String(value).trim().replace(",", ".");
    if (!/^\d{1,9}(\.\d{1,2})?$/.test(text))
      throw new Error("Укажите цену с точностью до копеек");
    const [whole, fraction = ""] = text.split(".");
    const result = Number(whole) * 100 + Number(fraction.padEnd(2, "0"));
    if (result > 10000000000) throw new Error("Цена слишком большая");
    return result;
  }
  function price(listing) {
    if (listing.price_mode === "exchange") return "Обмен";
    if (listing.price_mode !== "price") return "Не указано";
    if (listing.price_minor === 0) return "Бесплатно";
    return (
      new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 2 }).format(
        listing.price_minor / 100,
      ) + " ₽"
    );
  }
  const utils = { escape, minor, price };
  if (typeof module !== "undefined" && module.exports) module.exports = utils;
  else root.DormHubUtils = utils;
})(globalThis);
