const test=require('node:test'),assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path'),os=require('node:os');
const {spawnSync}=require('node:child_process');
const dashboard=JSON.parse(fs.readFileSync(path.join(__dirname,'../deploy/monitoring/grafana/dashboards/dorm-hub-http.json'),'utf8'));
test('HTTP dashboard consistently filters route and method and has full per-endpoint statistics',()=>{
 assert.equal(dashboard.uid,'dorm-hub-http');
 assert.deepEqual(dashboard.templating.list.map(v=>v.name),['scope','instance','method','route']);
 assert.equal(dashboard.refresh,'30s');
 for(const p of dashboard.panels)for(const t of p.targets){
  assert.match(t.expr,/method=~"\$\{method:regex\}"/);
  assert.match(t.expr,/route=~"\$\{route:regex\}"/);
  assert.match(t.expr,/instance=~"\$\{instance:regex\}"/);
 }
 const table=dashboard.panels.find(p=>p.type==='table');assert.ok(table);assert.equal(table.targets.length,1);assert.equal(table.targets[0].format,'table');assert.equal(table.targets[0].instant,true);
 for(const column of ['Запросы','2xx','3xx','4xx','5xx','Среднее, мс','p50, мс','p95, мс','p99, мс','Ошибки, %','Запросы/с'])assert.ok(table.targets[0].expr.includes('"'+column+'"'),column);
 assert.deepEqual(table.transformations.map(t=>t.id),['groupingToMatrix','organize']);
});
test('navigation exposes a separate HTTP dashboard while retaining server monitoring',()=>{
 const {render}=require('../web/assets/admin-analytics.js');
 const data={days:7,started_at:'2026-11-02T00:00:00Z',series:[],listings:[],monitoring_enabled:true};
 let html=render(data);assert.match(html,/href="\/monitoring\/d\/dorm-hub-http\/dorm-hub-api"/);assert.match(html,/href="\/monitoring\/"/);
 html=render({...data,monitoring_enabled:false});assert.doesNotMatch(html,/href="\/monitoring/);
});
function fixture(t){
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'dormhub-http-'));t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
 const folder=path.join(root,'dashboards');fs.mkdirSync(folder);
 const target=path.join(folder,'dorm-hub-http.json');fs.writeFileSync(target,'{"uid":"previous"}\n');
 fs.writeFileSync(path.join(folder,'server.json'),'server dashboard retained');
 let source=fs.readFileSync(path.join(__dirname,'../deploy/monitoring/update-http-dashboard.sh'),'utf8').replace(/^\[\[ \$EUID == 0 \]\].*\n/m,'').replace('/opt/dorm-hub',root).replace('/etc/dorm-hub-monitoring/config/grafana/dashboards',folder);
 const script=path.join(root,'update.sh');fs.writeFileSync(script,source);
 const bin=path.join(root,'bin');fs.mkdirSync(bin);
 fs.writeFileSync(path.join(bin,'git'),'#!/bin/sh\nif [ "$TEST_GIT_FAIL" = 1 ]; then exit 1; fi\ncat "$TEST_JSON"\n');fs.chmodSync(path.join(bin,'git'),0o700);
 const json=path.join(root,'next.json');fs.writeFileSync(json,JSON.stringify(dashboard));
 return {folder,target,root,run(fail=false){return spawnSync('bash',[script],{encoding:'utf8',env:{...process.env,PATH:bin+':'+process.env.PATH,TEST_GIT_FAIL:fail?'1':'0',TEST_JSON:json}});}};
}
test('dashboard install updates only its JSON, preserves the previous file and leaves server dashboard intact',t=>{
 const f=fixture(t),r=f.run();assert.equal(r.status,0,r.stderr);assert.equal(JSON.parse(fs.readFileSync(f.target,'utf8')).uid,'dorm-hub-http');assert.equal(fs.statSync(f.target).mode&0o777,0o644);
 assert.equal(fs.readFileSync(f.target+'.previous','utf8'),'{"uid":"previous"}\n');assert.equal(fs.readFileSync(path.join(f.folder,'server.json'),'utf8'),'server dashboard retained');
});
test('failed Git read does not replace the working dashboard',t=>{
 const f=fixture(t),r=f.run(true);assert.notEqual(r.status,0);assert.equal(fs.readFileSync(f.target,'utf8'),'{"uid":"previous"}\n');assert.ok(!fs.readdirSync(f.folder).some(n=>n.startsWith('.http-dashboard.')));
});
