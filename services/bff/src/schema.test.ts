import test from 'node:test';
import assert from 'node:assert/strict';
import { complexityAllowed,execute } from './schema.js';
test('query complexity accepts bounded read',()=>assert.equal(complexityAllowed('{ discover { items { nickname } } }'),true));
test('mutations and recursive fragments rejected',()=>{assert.equal(complexityAllowed('mutation { x }'),false);assert.equal(complexityAllowed('{ ...X } fragment X on Query { ...X }'),false);assert.equal(complexityAllowed('{ a:__typename } { b:__typename }'),false);});
test('real name impossible on dating type',async()=>{const r=await execute('{ discover { items { real_name } } }',{},async()=>({items:[],quiet:true}));assert.ok(r.errors);});
test('feed uses backend audience gate',async()=>{let called='';const r=await execute('{ feed(township:"kamayut") { items { id body } } }',{},async p=>{called=p;return {items:[],next_cursor:null};});assert.equal(r.errors,undefined);assert.equal(called,'/v1/feed?township=kamayut');});
test('people cannot query dating fields',async()=>{const r=await execute('{ people(q:"cherry") { items { nickname } } }',{},async()=>({items:[]}));assert.ok(r.errors);});
