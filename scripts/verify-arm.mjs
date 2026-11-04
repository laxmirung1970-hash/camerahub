import {execFileSync} from 'node:child_process';
import {mkdirSync,readFileSync,writeFileSync} from 'node:fs';
import {resolve,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
import {randomBytes} from 'node:crypto';
import assert from 'node:assert/strict';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');const cache=resolve(root,'.cache/arm-check');mkdirSync(cache,{recursive:true});const report=[];
for(const [image,binaries] of [['camerahub/app:1.0.0-arm64',['/usr/local/bin/camerahub']],['camerahub/gateway:1.9.14-control-arm64',['/usr/local/bin/go2rtc','/usr/bin/ffmpeg','/lib/ld-musl-aarch64.so.1']]]){
 const arch=execFileSync('docker',['image','inspect','--format','{{.Architecture}}',image],{encoding:'utf8'}).trim();assert.equal(arch,'arm64');const name='camerahub-elf-check-'+randomBytes(5).toString('hex');let created=false;
 try{execFileSync('docker',['create','--platform','linux/arm64','--name',name,image],{stdio:'pipe'});created=true;for(const binary of binaries){const file=resolve(cache,name+'-'+binary.split('/').pop());execFileSync('docker',['cp',name+':'+binary,file],{stdio:'pipe'});const elf=readFileSync(file);assert.equal(elf.subarray(0,4).toString('hex'),'7f454c46');assert.equal(elf.readUInt16LE(18),183,'Must be AArch64 ELF');report.push({image,binary,architecture:arch,machine:183,checked:true,executedOnNAS:false})}}finally{if(created)execFileSync('docker',['rm',name],{stdio:'pipe'})}
}
mkdirSync(resolve(root,'test-results'),{recursive:true});writeFileSync(resolve(root,'test-results/arm64.json'),JSON.stringify(report,null,2));console.log('ARM64 image metadata, Go binaries, FFmpeg and musl ELF: PASS (not executed on NAS)');
