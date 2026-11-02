// Generate semantic Prometheus tests directly from the dashboard's expressions.
// Usage in CI: node tests/http-promql.cjs OUTPUT_JSON
const fs=require('node:fs'),path=require('node:path');
const d=JSON.parse(fs.readFileSync(path.join(__dirname,'../deploy/monitoring/grafana/dashboards/dorm-hub-http.json'),'utf8'));
function query(expr,method='.*'){
 for(const [a,b] of [['${instance:regex}','.*'],['${method:regex}',method],['${route:regex}','.*'],['${scope:raw}','(.* )?/api/.*'],['$__rate_interval','2m'],['$__range_s','300'],['$__range','5m']])expr=expr.replaceAll(a,b);
 return expr;
}
const input=[];
function series(metric,method,route,values,extra=''){
 input.push({series:metric+'{job="dorm-hub",instance="test",method="'+method+'",route="'+route+'"'+extra+'}',values});
}
for(const [method,route,count,sum,buckets]of [['GET','GET /api/listings',7,.007,[['0.001',4],['0.0025',7],['+Inf',7]]],['POST','POST /api/auth/request',1,.002,[['0.001',0],['0.0025',1],['+Inf',1]]]]){
 series('dormhub_http_requests_total',method,route,'0+'+(method==='GET'?6:1)+'x10',',status="'+(method==='GET'?'200':'500')+'"');
 if(method==='GET')series('dormhub_http_requests_total',method,route,'0+1x10',',status="404"');
 series('dormhub_http_request_duration_seconds_count',method,route,'0+'+count+'x10');
 series('dormhub_http_request_duration_seconds_sum',method,route,'0+'+sum+'x10');
 for(const [le,n]of buckets)series('dormhub_http_request_duration_seconds_bucket',method,route,'0+'+n+'x10',',le="'+le+'"');
}
const table=query(d.panels.find(p=>p.type==='table').targets[0].expr);
const checks=[
 {expr:query(d.panels[0].targets[0].expr),eval_time:'5m',exp_samples:[{labels:'{}',value:80}]},
 {expr:query(d.panels[1].targets[0].expr),eval_time:'5m',exp_samples:[{labels:'{}',value:1.125}]},
 {expr:query(d.panels[0].targets[0].expr,'POST'),eval_time:'5m',exp_samples:[{labels:'{}',value:10}]},
];
for(const [measure,values]of [['Запросы',[70,10]],['2xx',[60,0]],['4xx',[10,0]],['5xx',[0,10]],['Среднее, мс',[1,2]]]){
 checks.push({expr:'('+table+') and on(measure) label_replace(vector(1), "measure", "'+measure+'", "__name__", ".*")',eval_time:'5m',exp_samples:['GET /api/listings','POST /api/auth/request'].map((endpoint,i)=>({labels:'{endpoint="'+endpoint+'",measure="'+measure+'"}',value:values[i]}))});
}
const fixture={fuzzy_compare:true,rule_files:[],evaluation_interval:'30s',tests:[{interval:'30s',input_series:input,promql_expr_test:checks}]};
if(!process.argv[2])throw new Error('Pass the output JSON path');
fs.writeFileSync(process.argv[2],JSON.stringify(fixture));
