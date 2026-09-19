#!/usr/bin/env python3
"""Public source checks; --demo explicitly issues one credential kept only in memory."""
import argparse, datetime, hashlib, json, os, time, urllib.request, urllib.parse, urllib.error
from pathlib import Path

def request(url, payload=None, token=None):
    headers={'User-Agent':'Astar-MCP-P0','Accept':'application/json'}
    if token: headers['Authorization']='Bearer '+token
    if payload is not None: headers['Content-Type']='application/json'
    req=urllib.request.Request(url,data=None if payload is None else json.dumps(payload).encode(),headers=headers)
    with urllib.request.urlopen(req,timeout=25) as r:
        raw=r.read(8*1024*1024+1)
        if len(raw)>8*1024*1024: raise ValueError('oversized response')
        return raw

def main():
    p=argparse.ArgumentParser();p.add_argument('--demo',action='store_true');p.add_argument('--output',default='.local/evidence/sources.json');args=p.parse_args()
    checks=[]
    def check(name,fn):
        try: checks.append({'check':name,'status':'passed','evidence':fn()})
        except Exception as e: checks.append({'check':name,'status':'failed','error':'HTTP '+str(e.code) if isinstance(e,urllib.error.HTTPError) else type(e).__name__})
    def contract():
        raw=request('https://docs.aradia.app/openapi.yaml')
        assert b'/v1/indexer/status:' in raw
        return {'sha256':hashlib.sha256(raw).hexdigest(),'bytes':len(raw)}
    check('aradia_contract',contract)
    def directory():
        seen=set();eligible=0;total=None;pages=None
        for page in range(1,101):
            q=urllib.parse.urlencode({'populate':'project_categories,project_chains','pagination[page]':page,'pagination[pageSize]':100,'sort[0]':'id:asc'})
            data=json.loads(request('https://strapi-prod.astar.network/api/projects?'+q));meta=data['meta']['pagination']
            if total is None: total,pages=meta['total'],meta['pageCount']
            assert total==meta['total'] and pages==meta['pageCount']
            for x in data['data']:
                assert x['id'] not in seen
                seen.add(x['id'])
                chains={c['attributes']['name'] for c in x['attributes']['project_chains']['data']}
                eligible+=bool(chains & {'Astar Network','Soneium'})
            if page>=pages: break
        assert len(seen)==total
        return {'total':total,'eligible':eligible,'pages':pages}
    check('ecosystem_all_pages',directory)
    def network():
        def rpc(method,params):
            d=json.loads(request(os.environ.get('ASTAR_RPC_URL','https://astar-rpc.n.dwellir.com'),{'jsonrpc':'2.0','id':1,'method':method,'params':params}))
            if 'error' in d: raise ValueError('RPC unavailable')
            return d['result']
        assert rpc('system_chain',[])=='Astar'
        block=rpc('chain_getFinalizedHead',[]);genesis=rpc('chain_getBlockHash',[0]);v=rpc('state_getRuntimeVersion',[block]);assert v['specName']=='astar'
        return {'genesis_hash':genesis,'finalized_block':block,'spec_version':v['specVersion'],'transaction_version':v['transactionVersion'],'code_correspondence':'unknown'}
    check('astar_finalized_runtime',network)
    token=os.environ.get('ARADIA_API_TOKEN')
    if not token and args.demo:
        def demo():
            nonlocal token
            d=json.loads(request('https://api.aradia.app/v1/demo-token',{}))['data'];token=d['token']
            return {'credential':'ephemeral; not persisted','scopes':d.get('scopes',[])}
        check('aradia_demo',demo)
    if token:
        def get(path):
            time.sleep(1.1)
            return json.loads(request('https://api.aradia.app'+path,token=token))
        collections=[];nfts=[]
        def listed(path,collect=None):
            d=get(path)
            if collect is not None: collect.extend(d['data'])
            assert isinstance(d['data'],list) and 'coverage' in d and 'pagination' in d
            return {'count':len(d['data']),'coverage':d.get('coverage'),'has_more':d['pagination']['has_more'], 'unknown_confirmations':sum(x.get('confirmations') is None for x in d['data']) if '/transfers' in path or '/activity' in path else None}
        check('aradia_collections',lambda:listed('/v1/collections?limit=1&network=astar',collections))
        def pagination():
            first=get('/v1/collections?limit=1&network=astar')
            if not first['pagination']['has_more']: return {'has_more':False}
            params={'limit':1,'network':'astar',**first['pagination']['next_page_params']}
            second=get('/v1/collections?'+urllib.parse.urlencode(params))
            assert second['data'] and second['data'][0]['contract_address']!=first['data'][0]['contract_address']
            return {'next_page_verified':True}
        check('aradia_pagination',pagination)
        check('aradia_status',lambda:get('/v1/indexer/status?network=astar'))
        check('aradia_activity',lambda:listed('/v1/activity?limit=1&network=astar'))
        if collections:
            a=collections[0]['contract_address']
            check('aradia_collection',lambda:{'fields':sorted(get('/v1/collections/'+a)['data'])})
            check('aradia_collection_nfts',lambda:listed('/v1/collections/'+a+'/nfts?limit=1',nfts))
            check('aradia_collection_transfers',lambda:listed('/v1/collections/'+a+'/transfers?limit=1'))
            if nfts:
                n=nfts[0];t=str(n['token_id'])
                check('aradia_nft',lambda:{'fields':sorted(get('/v1/nfts/'+a+'/'+t)['data'])})
                check('aradia_nft_transfers',lambda:listed('/v1/nfts/'+a+'/'+t+'/transfers?limit=1'))
                if n.get('current_owner'): check('aradia_owner_nfts',lambda:listed('/v1/owners/'+n['current_owner']+'/nfts?limit=1'))
            else: checks.append({'check':'aradia_nft_and_owner','status':'blocked','reason':'sample has no NFTs'})
        else: checks.append({'check':'aradia_sample','status':'blocked','reason':'no collection available'})
    else: checks.append({'check':'aradia_authenticated','status':'blocked','reason':'no credential'})
    out=Path(args.output);out.parent.mkdir(parents=True,exist_ok=True)
    out.write_text(json.dumps({'checked_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'checks':checks},indent=2)+'\n')
    for c in checks: print(c['check'],c['status'])
    return int(any(c['status']!='passed' for c in checks))
if __name__=='__main__': raise SystemExit(main())
