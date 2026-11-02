const test=require('node:test');
const assert=require('node:assert/strict');
const {render,chart}=require('../web/assets/admin-analytics.js');
function data(overrides={}) {return {days:7,started_at:'2026-11-02T00:00:00Z',dau:2,wau:3,mau:4,publications:1,views:5,contacts:2,pending:1,oldest_pending_seconds:3660,published:4,new_users:1,successful:0,monitoring_enabled:true,series:[{date:'2026-11-02',active_users:2,publications:1,views:5,contacts:2}],listings:[],offset:0,limit:20,total_listings:0,...overrides};}
test('analytics renders periods, distinct activity, monitor link and empty state',()=>{
 const html=render(data());assert.match(html,/days=30/);assert.match(html,/Активны сегодня/);assert.match(html,/href="\/monitoring\/"/);assert.match(html,/1 ч 1 мин/);assert.match(html,/пока не зафиксированы/);assert.doesNotMatch(html,/NaN|undefined/);
 const disabled=render(data({monitoring_enabled:false}));assert.doesNotMatch(disabled,/href="\/monitoring\/"/);
});
test('listing titles and category names stay text; pagination keeps period',()=>{
 const html=render(data({listings:[{id:3,title:'<img src=x onerror=alert(1)>',category:'test',views:1,contacts:1}],total_listings:41,offset:20}),[{code:'test',name:'<script>test</script>'}]);
 assert.doesNotMatch(html,/<img|<script>/);assert.match(html,/&lt;img/);assert.match(html,/&lt;script/);assert.match(html,/tab=analytics&days=7&offset=40/);assert.match(html,/#\/listing\/3/);
});
test('zero and single-day charts are finite and include readable data',()=>{
 assert.match(chart([]),/Нет данных/);
 for(const n of [0,1,100]){const html=chart([{date:'2026-11-02',active_users:n,publications:0}]);assert.doesNotMatch(html,/NaN|Infinity/);assert.match(html,/role="img"/);assert.match(html,/02.11/);}
});
