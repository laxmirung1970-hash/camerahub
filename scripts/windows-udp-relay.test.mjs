import {test} from 'node:test';
import assert from 'node:assert/strict';
import net from 'node:net';
import {EventEmitter, once} from 'node:events';
import {createRelay} from './windows-udp-relay.mjs';

const token = 'a'.repeat(64), host = '172.16.0.202';
async function connection(relay) {
  const client=net.connect(relay.server.address().port,'127.0.0.1');
  client.on('error',()=>{});await once(client,'connect');return client;
}
test('relay rejects incorrect authentication and unregistered destinations',async()=>{
  let sockets=0;
  const relay=createRelay({token,hosts:[host],port:0,socketFactory:()=>{sockets++;return new EventEmitter()}});await relay.listen();
  try{for(const hello of [{token:'b'.repeat(64),host},{token,host:'172.16.0.203'},null]){
    const c=await connection(relay);const closed=once(c,'close');c.write(JSON.stringify(hello)+'\n');await closed;
  }assert.equal(sockets,0);}finally{relay.close()}
});
test('relay carries framed datagrams and learns ports only from the selected camera',async()=>{
  let udp;
  const relay=createRelay({token,hosts:[host],port:0,socketFactory:()=>{
    udp=new EventEmitter();udp.close=()=>{};udp.send=(packet,port,address,done)=>{udp.emit('sent',{packet,port,address});done()};return udp;
  }});await relay.listen();
  const c=await connection(relay);
  try{
    let got=once(c,'data');c.write(JSON.stringify({token,host})+'\n');assert.equal((await got)[0][0],1);
    const sent=once(udp,'sent');c.write(Buffer.from([0x7d,0x6c,0,4,0xf1,0x30,0,0]));
    const [packet]=await sent;assert.equal(packet.port,32108);assert.equal(packet.address,host);assert.deepEqual([...packet.packet],[0xf1,0x30,0,0]);
    got=once(c,'data');udp.emit('message',Buffer.from([0xf1,0x41,0,0]),{address:host,port:10298});
    assert.deepEqual([...(await got)[0]],[0x28,0x3a,0,4,0xf1,0x41,0,0]);
    const closed=once(c,'close');c.write(Buffer.from([0,53,0,4,0xf1,0x30,0,0]));await closed;
  }finally{c.destroy();relay.close()}
});
test('relay reads a refreshed registry allowlist for each new client',async()=>{
 let hosts=[host],sockets=0;
 const relay=createRelay({token,hosts,getHosts:()=>hosts,port:0,socketFactory:()=>{sockets++;const u=new EventEmitter();u.close=()=>{};return u}});await relay.listen();
 try{hosts=[];const c=await connection(relay);const closed=once(c,'close');c.write(JSON.stringify({token,host})+'\n');await closed;assert.equal(sockets,0)}finally{relay.close()}
});
