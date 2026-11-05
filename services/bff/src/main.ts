import { createServer } from 'node:http';
import { execute } from './schema.js';
const upstream = new URL(process.env.API_UPSTREAM ?? 'http://127.0.0.1:8080');
if(!['http:','https:'].includes(upstream.protocol))throw new Error('invalid upstream');
const origin=process.env.ALLOWED_ORIGIN ?? 'http://localhost:3000';
const server=createServer(async(req,res)=>{
 res.setHeader('Content-Type','application/json');res.setHeader('Cache-Control','no-store');res.setHeader('X-Content-Type-Options','nosniff');
 const send=(status:number,data:unknown)=>{res.writeHead(status);res.end(JSON.stringify(data));};
 if(req.headers.origin && req.headers.origin!==origin){send(403,{error:'origin_denied'});return;}
 if(req.headers.origin){res.setHeader('Access-Control-Allow-Origin',origin);res.setHeader('Vary','Origin');}
 if(req.method==='OPTIONS'){res.setHeader('Access-Control-Allow-Headers','Authorization,Content-Type');res.setHeader('Access-Control-Allow-Methods','POST');send(200,{});return;}
 if(req.url==='/health/live'){send(200,{status:'alive'});return;}
 if(req.url!=='/graphql'||req.method!=='POST'){send(404,{error:'not_found'});return;}
 if(!req.headers.authorization?.startsWith('Bearer ')){send(401,{error:'authentication_required'});return;}
 if(!req.headers['content-type']?.startsWith('application/json')){send(415,{error:'json_required'});return;}
 try{
  const chunks:Buffer[]=[];let size=0;
  for await(const chunk of req){const b=Buffer.from(chunk);size+=b.length;if(size>16384){send(413,{error:'body_too_large'});return;}chunks.push(b);}
  const body=JSON.parse(Buffer.concat(chunks).toString()) as {query?:unknown;variables?:unknown};
  if(typeof body.query!=='string'||(body.variables!==undefined&&(typeof body.variables!=='object'||body.variables===null||Array.isArray(body.variables)))){send(400,{error:'invalid_request'});return;}
  // Validate the live session even for introspection / __typename-only queries.
  const auth=await fetch(new URL('/v1/auth/sessions',upstream),{headers:{Authorization:req.headers.authorization},signal:AbortSignal.timeout(5000)});
  if(auth.status!==200){send(auth.status===401?401:503,{error:'upstream_auth_unavailable'});return;}
  const read=async(path:string)=>{const r=await fetch(new URL(path,upstream),{headers:{Authorization:req.headers.authorization!},signal:AbortSignal.timeout(5000)});if(!r.ok)throw new Error('upstream_read_failed');return r.json();};
  const result=await execute(body.query,(body.variables??{}) as Record<string,unknown>,read);
  // Do not send stack traces or upstream response bodies.
  if(result.errors){send(400,{data:('data' in result ? result.data : null),errors:result.errors.map(()=>({message:'query_failed'}))});return;}
  send(200,result);
 }catch{send(400,{error:'request_failed'});}
});
server.headersTimeout=10000;server.requestTimeout=15000;
server.listen(Number(process.env.PORT??8081),'127.0.0.1',()=>console.log('Nexus GraphQL reader listening'));
for(const signal of ['SIGINT','SIGTERM'])process.on(signal,()=>server.close(()=>process.exit(0)));
