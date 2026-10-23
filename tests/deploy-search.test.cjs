const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {spawnSync} = require('node:child_process');

// Exercise the installer in a temporary tree with a fake Docker CLI. The real
// root directories, Docker daemon and SSH configuration are never accessed.
function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'dormhub-search-'));
  t.after(() => fs.rmSync(root, {recursive: true, force: true}));
  const project = path.join(root, 'project'), config = path.join(root, 'config');
  const state = path.join(root, 'state'), bin = path.join(root, 'bin'), deploy = path.join(project, 'deploy');
  for (const dir of [deploy, config, state, bin]) fs.mkdirSync(dir, {recursive: true});
  const original = 'previous server compose\n';
  fs.writeFileSync(path.join(config, 'compose.registry.yaml'), original);
  fs.writeFileSync(path.join(state, 'current-image'), 'dormhub-local-image\n');
  fs.writeFileSync(path.join(project, '.env.production'), 'SMTP_HOST=test-only\n');
  const source = path.join(__dirname, '../deploy');
  for (const name of ['compose.registry.yaml', 'server-deploy.sh']) fs.copyFileSync(path.join(source, name), path.join(deploy, name));
  let script = fs.readFileSync(path.join(source, 'install-server.sh'), 'utf8');
  script = script.replace(/^\[\[ \$EUID == 0 \]\].*\n/m, '');
  for (const [from, to] of [['/opt/dorm-hub',project], ['/etc/dorm-hub',config],
    ['/var/lib/dorm-hub-deploy',state], ['/root/.ssh',path.join(root,'ssh')],
    ['/run/lock/dorm-hub-deploy.lock',path.join(root,'lock')],
    ['/usr/local/sbin/dorm-hub-deploy',path.join(root,'installed-deploy')]]) script = script.replaceAll(from,to);
  const installer = path.join(deploy,'install-server.sh'); fs.writeFileSync(installer,script);
  fs.writeFileSync(path.join(bin,'docker'), `#!/usr/bin/env bash
set -eu
printf '%s\\n' "$*" >> "$DEPLOY_TEST_LOG"
last=''
for argument in "$@"; do last="$argument"; done
if [[ "$*" == *" up "* && "$last" == "\${DEPLOY_TEST_FAIL:-}" && ! -e "$DEPLOY_TEST_MARKER" ]]; then
  touch "$DEPLOY_TEST_MARKER"
  exit 1
fi
`);
  fs.writeFileSync(path.join(bin,'flock'), '#!/usr/bin/env bash\nexit 0\n');
  fs.chmodSync(path.join(bin,'docker'),0o700);fs.chmodSync(path.join(bin,'flock'),0o700);
  const log = path.join(root,'docker.log');
  return {project,config,state,original,log,run(fail='',args=['--search']) {
    return spawnSync('bash',[installer,...args],{encoding:'utf8',env:{...process.env,PATH:bin+path.delimiter+process.env.PATH,
      DEPLOY_TEST_LOG:log,DEPLOY_TEST_FAIL:fail,DEPLOY_TEST_MARKER:path.join(root,'failed-once')}});
  }};
}
test('search installer generates one persistent key and only recreates the app and search service', t => {
  const f=fixture(t);let result=f.run();assert.equal(result.status,0,result.stderr);
  const env=fs.readFileSync(path.join(f.project,'.env.production'),'utf8');
  const key=env.match(/^MEILI_MASTER_KEY=([a-f0-9]{64})$/m)?.[1];assert.ok(key);
  assert.ok(!result.stdout.includes(key));
  result=f.run();assert.equal(result.status,0,result.stderr);
  assert.equal(fs.readFileSync(path.join(f.project,'.env.production'),'utf8'),env);
  const calls=fs.readFileSync(f.log,'utf8');
  assert.match(calls,/up .*meilisearch/);assert.match(calls,/up .*app/);
  assert.doesNotMatch(calls,/\bdown\b|\bvolume rm\b|up .*\bdb$/m);
  assert.equal(fs.readFileSync(path.join(f.state,'current-image'),'utf8'),'dormhub-local-image\n');
});
for (const fail of ['meilisearch','app']) test(`search installer restores the previous configuration after ${fail} failure`,t => {
  const f=fixture(t),result=f.run(fail);assert.equal(result.status,1);
  assert.equal(fs.readFileSync(path.join(f.config,'compose.registry.yaml'),'utf8'),f.original);
  const calls=fs.readFileSync(f.log,'utf8');assert.match(calls,/up .*app/);
  assert.doesNotMatch(calls,/\bdown\b|\bvolume rm\b/);
});
test('installer rejects unexpected options before changing the environment',t=>{
  const f=fixture(t),result=f.run('',['unexpected']);assert.equal(result.status,2);
  assert.equal(fs.readFileSync(path.join(f.project,'.env.production'),'utf8'),'SMTP_HOST=test-only\n');
});
