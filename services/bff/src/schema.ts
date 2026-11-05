import { buildSchema, graphql, parse, visit, Kind } from 'graphql';
export const schema = buildSchema(`
 type DatingCard { profile_id: ID!, nickname: String!, age: Int!, bio: String!, interests: [String!]!, distance_bucket: String, township: String!, tier: Int! }
 type DatingConnection { items: [DatingCard!]!, quiet: Boolean!, effective_band: Int }
 type Post { id: ID!, author: String!, body: String!, township: String!, audience: String!, status: String!, reaction_count: Int!, comment_count: Int!, share_count: Int! }
 type FeedConnection { items: [Post!]!, next_cursor: String }
 type SocialProfile { user_id: ID!, real_name: String!, township: String!, bio: String! }
 type SocialSearch { items: [SocialProfile!]! }
 type Query { discover: DatingConnection!, feed(cursor: String, township: String): FeedConnection!, people(q: String!): SocialSearch! }
`);
export type Reader = (path: string) => Promise<unknown>;
export function complexityAllowed(query: string): boolean {
 if (query.length > 10000) return false;
 try {
  const doc=parse(query);let fields=0,depth=0,maxDepth=0,valid=true,operations=0;
  visit(doc,{
   OperationDefinition(n){operations++;if(n.operation!=='query')valid=false;},
   Field:{enter(){fields++;depth++;maxDepth=Math.max(maxDepth,depth);},leave(){depth--; }},
   // No fragment expansion ambiguity; persisted fragments can be introduced after a real cost model.
   FragmentDefinition(){valid=false;},FragmentSpread(){valid=false;},InlineFragment(){valid=false;}
  });
  return valid && operations===1 && fields<=80 && maxDepth<=6 && doc.definitions.every(d=>d.kind===Kind.OPERATION_DEFINITION);
 } catch { return false; }
}
export async function execute(query: string, variables: Record<string,unknown>, read: Reader) {
 if (!complexityAllowed(query)) return {errors:[{message:'query_rejected'}]};
 const root={
  discover:()=>read('/v1/discover'),
  feed:({cursor,township}:{cursor?:string;township?:string})=>read('/v1/feed?'+new URLSearchParams({...cursor?{cursor}:{},...township?{township}:{}})),
  people:({q}:{q:string})=>read('/v1/search?'+new URLSearchParams({q,type:'people'}))
 };
 return graphql({schema,source:query,rootValue:root,variableValues:variables});
}
