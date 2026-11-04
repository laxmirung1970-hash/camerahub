import {readFileSync,writeFileSync,mkdirSync,readdirSync} from 'node:fs';
import {resolve,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');const out=resolve(root,'vendor/web-notices');mkdirSync(out,{recursive:true});
for(const name of ['react','react-dom','hls.js','lucide-react']){const dir=resolve(root,'web/node_modules',name);const files=readdirSync(dir).filter(f=>/^(LICENSE|COPYING|NOTICE)/i.test(f));for(const file of files)writeFileSync(resolve(out,name+'-'+file),readFileSync(resolve(dir,file)));}
console.log('Web dependency notices collected.');
