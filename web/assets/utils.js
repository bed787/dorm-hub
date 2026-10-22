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
  function linkify(value) {
    const text = String(value ?? "");
    const pattern = /(^|[\s([{«])((?:https?:\/\/|www\.)[^\s<>"'\u0000-\u001f]+)/giu;
    let result = "", cursor = 0;
    for (const match of text.matchAll(pattern)) {
      let url = match[2];
      // Leave sentence punctuation and unmatched closing brackets outside.
      while (url) {
        const last = url.at(-1), pairs = { ")": "(", "]": "[", "}": "{" };
        if (/[.,!?:;…»]/u.test(last) || (pairs[last] &&
          url.split(last).length > url.split(pairs[last]).length)) url = url.slice(0, -1);
        else break;
      }
      const start = match.index + match[1].length;
      result += escape(text.slice(cursor, start));
      let target;
      try { target = new URL(/^www\./i.test(url) ? "https://" + url : url); } catch {}
      result += target && ["http:", "https:"].includes(target.protocol) &&
        !target.username && !target.password
        ? `<a href="${escape(target.href)}" target="_blank" rel="noopener noreferrer">${escape(url)}</a>`
        : escape(url);
      cursor = start + url.length;
    }
    return result + escape(text.slice(cursor));
  }
  const utils = { escape, minor, price, linkify };
  if (typeof module !== "undefined" && module.exports) module.exports = utils;
  else root.DormHubUtils = utils;
})(globalThis);
