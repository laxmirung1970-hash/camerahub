import {randomBytes} from 'node:crypto';
import {mkdirSync,writeFileSync,existsSync} from 'node:fs';
import {resolve,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
const dir=resolve(dirname(fileURLToPath(import.meta.url)),'../data/signing');
mkdirSync(dir,{recursive:true});const path=resolve(dir,'signing.env');
if(!existsSync(path))writeFileSync(path,`KEYSTORE_PASSWORD=${randomBytes(32).toString('hex')}\n`,{mode:0o600});
console.log('Private signing configuration ready (not included in releases).');
