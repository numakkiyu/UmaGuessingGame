import {createHash,randomUUID} from 'node:crypto';

/** Exercise the same bounded admission protocol as browser matchmaking. */
export async function createMatchingTicket(client,poolId='classic-duel') {
 const challenge=await client.call('POST','/matchmaking-proofs',{poolId},randomUUID());
 const proof=challenge.json??challenge.body;
 if(challenge.status!==201)throw new Error(`Match admission refused: ${challenge.status} ${proof?.code??''}`);
 const {challengeId,bits}=proof;
 if(typeof challengeId!=='string'||!Number.isInteger(bits)||bits<12||bits>20)throw new Error('Invalid match challenge');
 for(let i=0;i<2**26;i++){
  const nonce=String(i),digest=createHash('sha256').update(`${challengeId}:${nonce}`).digest();
  let valid=true;
  for(let bit=0;bit<bits;bit++)if(digest[bit>>3]&(1<<(7-(bit%8)))){valid=false;break;}
  if(valid)return client.call('POST','/matchmaking-tickets',{poolId,proof:{challengeId,nonce}},randomUUID());
 }
 throw new Error('Match admission work limit reached');
}
