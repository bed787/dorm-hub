const test = require('node:test');
const assert = require('node:assert/strict');
const {linkify} = require('../web/assets/utils.js');
test('description links preserve paragraphs, Cyrillic, query strings and sentence punctuation', () => {
  const result = linkify('Смотри https://example.org/лампа?a=1&b=2.\nИ www.example.org/test_(page).');
  assert.match(result, /href="https:\/\/example.org\/%D0%BB/);
  assert.match(result, /\?a=1&amp;b=2/);
  assert.match(result, />https:\/\/example.org\/лампа\?a=1&amp;b=2<\/a>\.\nИ /);
  assert.match(result, /href="https:\/\/www.example.org\/test_\(page\)"/);
  assert.match(result, /<\/a>\.$/);
});
test('link conversion escapes HTML, excludes dangerous schemes and cannot inject attributes', () => {
  const result = linkify('<img src=x onerror="alert(1)"> javascript:alert(1) data:text/html,x https://example.org/" onclick="bad()');
  assert.doesNotMatch(result, /<img|<script|href="(?:javascript|data):| onclick="/);
  assert.match(result, /&lt;img/);
  assert.match(result, /rel="noopener noreferrer"/);
  assert.equal((result.match(/<a /g) || []).length, 1);
  assert.doesNotMatch(linkify('https://name:password@example.org/ https://'), /<a /);
});
test('multiple links leave surrounding brackets and plain text intact', () => {
  const result = linkify('(https://example.org/a) [http://example.org/b].');
  assert.match(result, /^\(<a /);
  assert.match(result, /a<\/a>\) \[<a /);
  assert.match(result, /b<\/a>\]\.$/);
  assert.equal(linkify('Обычный текст\nбез ссылки'), 'Обычный текст\nбез ссылки');
});
