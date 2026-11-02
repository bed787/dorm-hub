"use strict";
(function(root){
 const h=root.DormHubUtils ? root.DormHubUtils.escape : value=>String(value??"").replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
 const number=value=>new Intl.NumberFormat('ru-RU').format(Math.max(0,Number(value)||0));
 function chart(series){
  if(!series.length)return '<p class="muted">Нет данных за этот период.</p>';
  const width=760,height=220,left=42,right=18,top=16,bottom=34;
  const max=Math.max(2,Math.ceil(Math.max(0,...series.flatMap(d=>[Number(d.active_users)||0,Number(d.publications)||0]))/2)*2);
  const x=i=>left+(width-left-right)*(series.length===1?.5:i/(series.length-1));
  const y=n=>height-bottom-(height-top-bottom)*Math.max(0,Number(n)||0)/max;
  const lines=[0,.5,1].map(t=>`<line x1="${left}" y1="${y(max*t)}" x2="${width-right}" y2="${y(max*t)}" class="analytics-grid-line"/><text x="${left-8}" y="${y(max*t)+4}" text-anchor="end">${number(Math.ceil(max*t))}</text>`).join('');
  const labels=[...new Set([0,Math.floor((series.length-1)/2),series.length-1])].map(i=>`<text x="${x(i)}" y="${height-9}" text-anchor="middle">${h(series[i].date.slice(5).split('-').reverse().join('.'))}</text>`).join('');
  const plot=(key,cls)=>`<polyline class="${cls}" fill="none" points="${series.map((d,i)=>`${x(i)},${y(d[key])}`).join(' ')}"/>`+series.map((d,i)=>`<circle class="${cls}" cx="${x(i)}" cy="${y(d[key])}" r="3"><title>${h(d.date)}: ${number(d[key])}</title></circle>`).join('');
  return `<svg class="analytics-chart" viewBox="0 0 ${width} ${height}" role="img" aria-label="Активные пользователи и первые публикации по дням">${lines}${labels}${plot('active_users','analytics-users-line')}${plot('publications','analytics-publications-line')}</svg>`;
 }
 function render(data,categories=[]){
  const p=days=>`#/admin?tab=analytics&days=${days}`;
  const cards=[['Активны сегодня',data.dau],['Активны за 7 дней',data.wau],['Активны за 30 дней',data.mau],['Первые публикации',data.publications],['Просмотры',data.views],['Переходы к контактам',data.contacts]];
  const oldest=data.pending?Math.floor(data.oldest_pending_seconds/3600)+' ч '+Math.floor(data.oldest_pending_seconds%3600/60)+' мин':'—';
  const category=code=>categories.find(c=>c.code===code)?.name||code||'Без категории';
  const start=new Date(data.started_at).toLocaleDateString('ru-RU',{timeZone:'Europe/Moscow'});
  return `<section class="analytics-panel"><div class="analytics-toolbar"><nav class="tabs" aria-label="Период статистики">${[[1,'Сегодня'],[7,'7 дней'],[30,'30 дней']].map(([days,label])=>`<a href="${p(days)}" class="${data.days===days?'selected':''}">${label}</a>`).join('')}</nav>${data.monitoring_enabled?'<a class="button secondary" href="/monitoring/d/dorm-hub-http/dorm-hub-api" target="_blank" rel="noopener noreferrer">API: запросы и ошибки ↗</a><a class="button secondary" href="/monitoring/" target="_blank" rel="noopener noreferrer">Мониторинг сервера ↗</a>':'<span class="small muted">Мониторинг сервера пока не подключён.</span>'}</div>
  <div class="analytics-cards">${cards.map(([label,value])=>`<article class="panel analytics-stat"><span class="small muted">${label}</span><strong>${number(value)}</strong></article>`).join('')}</div>
  <div class="analytics-summary small"><span>Опубликовано сейчас: <strong>${number(data.published)}</strong></span><a href="#/admin?tab=listings">На проверке: <strong>${number(data.pending)}</strong></a><span>Самое долгое ожидание: <strong>${h(oldest)}</strong></span><span>Новые пользователи: <strong>${number(data.new_users)}</strong></span><span>Отмечено успешными: <strong>${number(data.successful)}</strong></span></div>
  <section class="panel analytics-graph"><h2>Активность по дням</h2><div class="analytics-legend small"><span class="analytics-users-key">Пользователи</span><span class="analytics-publications-key">Первые публикации</span></div>${chart(data.series)}<details><summary class="small muted">Показать числа</summary><div class="table-wrap"><table><thead><tr><th>Дата</th><th>Пользователи</th><th>Публикации</th><th>Просмотры</th><th>Контакты</th></tr></thead><tbody>${data.series.map(d=>`<tr><td>${h(d.date)}</td><td>${number(d.active_users)}</td><td>${number(d.publications)}</td><td>${number(d.views)}</td><td>${number(d.contacts)}</td></tr>`).join('')}</tbody></table></div></details></section>
  <section class="panel analytics-listings"><h2>Объявления за выбранный период</h2>${data.listings.length?`<div class="table-wrap"><table><thead><tr><th>Объявление</th><th>Просмотры</th><th>Переходы к контактам</th></tr></thead><tbody>${data.listings.map(l=>`<tr><td><a href="#/listing/${Number(l.id)}">${h(l.title||'Без названия')}</a><div class="small muted">${h(category(l.category))}</div></td><td>${number(l.views)}</td><td>${number(l.contacts)}</td></tr>`).join('')}</tbody></table></div>`:'<p class="muted">Просмотры и переходы пока не зафиксированы.</p>'}${data.total_listings>data.limit?`<nav class="pagination">${data.offset>0?`<a class="button plain" href="${p(data.days)}&offset=${Math.max(0,data.offset-data.limit)}">← Назад</a>`:''}<span class="small muted">${data.offset+1}–${Math.min(data.offset+data.limit,data.total_listings)} из ${data.total_listings}</span>${data.offset+data.limit<data.total_listings?`<a class="button plain" href="${p(data.days)}&offset=${data.offset+data.limit}">Далее →</a>`:''}</nav>`:''}</section>
  <p class="small muted analytics-note">Активность и переходы собираются с ${h(start)}. Дни считаются по московскому времени. Просмотр учитывается один раз в день для пользователя и объявления; переходы — отдельно для Telegram и VK. Действия администраторов и просмотры собственных объявлений исключены. Переход к контакту не подтверждает сделку.</p></section>`;
 }
 const exported={render,chart};if(typeof module!=='undefined'&&module.exports)module.exports=exported;else root.DormHubAnalytics=exported;
})(globalThis);
