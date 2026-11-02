const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),os=require('node:os'),path=require('node:path');
const {spawnSync}=require('node:child_process');
function fixture(t){
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'dormhub-monitoring-'));t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
 const dirs={project:root+'/project',config:root+'/app-config',monitor:root+'/monitor-config',state:root+'/state',bin:root+'/bin'};
 for(const dir of Object.values(dirs))fs.mkdirSync(dir,{recursive:true});
 fs.mkdirSync(dirs.project+'/deploy',{recursive:true});fs.cpSync(path.join(__dirname,'../deploy/monitoring'),dirs.project+'/deploy/monitoring',{recursive:true});
 fs.copyFileSync(path.join(__dirname,'../deploy/server-deploy.sh'),dirs.project+'/deploy/server-deploy.sh');
 const originalEnv='SMTP_PASSWORD=unchanged-test-password\nBASE_URL=https://dorm-hub.ru\n';
 fs.writeFileSync(dirs.project+'/.env.production',originalEnv);fs.writeFileSync(dirs.config+'/compose.registry.yaml','original custom compose\n');fs.writeFileSync(dirs.state+'/current-image','immutable-image\n');
 const installer=dirs.project+'/deploy/monitoring/install.sh';let script=fs.readFileSync(installer,'utf8').replace(/^\[\[ \$EUID == 0 \]\].*\n/m,'');
 for(const [from,to] of [['/etc/dorm-hub-monitoring',dirs.monitor],['/etc/dorm-hub',dirs.config],['/opt/dorm-hub',dirs.project],['/var/lib/dorm-hub-deploy',dirs.state],['/run/lock/dorm-hub-deploy.lock',root+'/lock'],['/usr/local/sbin/dorm-hub-deploy',root+'/deployer']])script=script.replaceAll(from,to);
 fs.writeFileSync(installer,script);
 const docker=`#!/usr/bin/env bash
set -eu
printf '%s\\n' "$*" >> "$TEST_MONITOR_LOG"
if [[ "$*" == inspect* ]]; then printf 'BASE_URL=https://dorm-hub.ru\\n'; fi
if [[ "$*" == *' ps -q app' ]]; then printf 'app-container\\n'; fi
if [[ "$*" == exec* ]]; then
  if [[ "\${TEST_MONITOR_FAIL:-}" == metrics ]]; then exit 1; fi
  printf '# HELP dormhub_http_requests_total Requests\\n'
fi
if [[ "$*" == *' up '* && "\${TEST_MONITOR_FAIL:-}" == stack && "$*" == *'--project-name dorm-hub-monitoring '* ]]; then exit 1; fi
if [[ "$*" == *' up '* && "\${TEST_MONITOR_FAIL:-}" == app && "$*" == *'compose.monitoring.yaml'* && ! -f "$TEST_MONITOR_MARKER" ]]; then touch "$TEST_MONITOR_MARKER"; exit 1; fi
`;
 for(const [name,body] of [['docker',docker],['flock','#!/bin/sh\nexit 0\n'],['curl','#!/bin/sh\nexit 0\n']]){fs.writeFileSync(dirs.bin+'/'+name,body);fs.chmodSync(dirs.bin+'/'+name,0o700);}
 const log=root+'/docker.log';
 return {root,dirs,log,originalEnv,run(fail='',args=[]){return spawnSync('bash',[installer,...args],{encoding:'utf8',env:{...process.env,PATH:dirs.bin+':'+process.env.PATH,TEST_MONITOR_LOG:log,TEST_MONITOR_FAIL:fail,TEST_MONITOR_MARKER:root+'/failed'}});}};
}
test('monitoring install preserves production env, volumes and Grafana credentials across retries',t=>{
 const f=fixture(t);let r=f.run();assert.equal(r.status,0,r.stderr);
 const secrets=fs.readFileSync(f.dirs.monitor+'/secrets.env','utf8');assert.match(secrets,/GRAFANA_ADMIN_PASSWORD=[a-f0-9]{48}/);assert.doesNotMatch(r.stdout,/GRAFANA_ADMIN_PASSWORD=/);
 r=f.run();assert.equal(r.status,0,r.stderr);assert.equal(fs.readFileSync(f.dirs.monitor+'/secrets.env','utf8'),secrets);
 assert.equal(fs.readFileSync(f.dirs.project+'/.env.production','utf8'),f.originalEnv);assert.equal(fs.readFileSync(f.dirs.config+'/compose.registry.yaml','utf8'),'original custom compose\n');
 const calls=fs.readFileSync(f.log,'utf8');assert.doesNotMatch(calls,/\bdown\b|volume rm| up .*\bdb$/m);assert.match(calls,/compose.monitoring.yaml.*up .*app/);
 assert.ok(fs.existsSync(f.root+'/deployer'));
});
for(const fail of ['stack','app','metrics'])test('monitoring install rolls application back after '+fail+' failure',t=>{
 const f=fixture(t),r=f.run(fail);assert.notEqual(r.status,0);
 assert.equal(fs.readFileSync(f.dirs.project+'/.env.production','utf8'),f.originalEnv);assert.ok(!fs.existsSync(f.dirs.config+'/compose.monitoring.yaml'));
 assert.equal(fs.readFileSync(f.dirs.config+'/compose.registry.yaml','utf8'),'original custom compose\n');assert.ok(!fs.existsSync(f.root+'/deployer'));
 const calls=fs.readFileSync(f.log,'utf8');assert.match(calls,/up .*app/);assert.doesNotMatch(calls,/\bdown\b|volume rm/);
});
test('monitoring update failure restores an existing overlay and config',t=>{
 const f=fixture(t);assert.equal(f.run().status,0);
 const before=fs.readFileSync(f.dirs.config+'/compose.monitoring.yaml','utf8');
 const config=fs.readFileSync(f.dirs.monitor+'/config/prometheus.yml','utf8');
 assert.notEqual(f.run('metrics').status,0);
 assert.equal(fs.readFileSync(f.dirs.config+'/compose.monitoring.yaml','utf8'),before);
 assert.equal(fs.readFileSync(f.dirs.monitor+'/config/prometheus.yml','utf8'),config);
});
