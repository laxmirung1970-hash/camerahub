import {spawnSync} from 'node:child_process';
import {mkdirSync,existsSync} from 'node:fs';
import {resolve,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
mkdirSync(resolve(root,'media'),{recursive:true});
const ffmpeg=process.env.FFMPEG||'ffmpeg';
for(let scene=1;scene<=4;scene++)for(const high of [false,true]){
 const target=resolve(root,`media/scene-${scene}-${high?'high':'low'}.mp4`);if(existsSync(target))continue;
 const scale=high?2:1,w=640*scale,h=360*scale;
 const colors=['0x566655','0x515b61','0x81765d','0x53665e'];
 const filters=[`drawbox=x=0:y=0:w=${w}:h=${60*scale}:color=0x25342e:t=fill`,`drawbox=x=${30*scale}:y=${70*scale}:w=${220*scale}:h=${210*scale}:color=0x38473e:t=fill`,`drawbox=x=${46*scale}:y=${86*scale}:w=${188*scale}:h=${178*scale}:color=0x76836b:t=fill`,`drawgrid=w=${80*scale}:h=${60*scale}:t=1:c=0xffffff08`,`drawbox=x=${270*scale}:y=${74*scale}:w=${330*scale}:h=${190*scale}:color=0x2c3a30:t=fill`,`drawbox=x=${284*scale}:y=${88*scale}:w=${302*scale}:h=${162*scale}:color=${colors[scene-1]}:t=fill`,`drawbox=x=${300*scale}:y=${110*scale}:w=${65*scale}:h=${110*scale}:color=0xb5b097:t=fill`,`drawbox=x=${460*scale}:y=${125*scale}:w=${105*scale}:h=${70*scale}:color=0x354c3d:t=fill`,`drawtext=fontfile='${resolve(root,'web/public/fonts/IBMPlexSansThai-Medium.ttf').replaceAll('\\','/').replace(':','\\:')}':text='DEMO 0${scene}   /   SYNTHETIC CAMERA':fontsize=${13*scale}:fontcolor=0xc7d4be:x=${20*scale}:y=${20*scale}`,`drawtext=fontfile='${resolve(root,'web/public/fonts/IBMPlexSansThai-Regular.ttf').replaceAll('\\','/').replace(':','\\:')}':text='NOT REAL CAMERA FOOTAGE':fontsize=${10*scale}:fontcolor=0xd4e3c780:x=${20*scale}:y=${323*scale}`];
 // A moving test target and a clock prove that this is decoded live video, not a still image.
 filters.push(`drawtext=fontfile='${resolve(root,'web/public/fonts/IBMPlexSansThai-Medium.ttf').replaceAll('\\','/').replace(':','\\:')}':text='●':fontsize=${35*scale}:fontcolor=0xe1b270:x='${50*scale}+mod(t*${30*scale},${510*scale})':y='${275*scale}-abs(sin(t))*${120*scale}'`);
 const args=['-hide_banner','-loglevel','error','-y','-f','lavfi','-i',`color=c=${colors[scene-1]}:s=${w}x${h}:r=15`,'-f','lavfi','-i',`sine=frequency=${240+scene*80}:sample_rate=48000`,'-t','12','-vf',filters.join(','),'-af','volume=0.025','-c:v','libx264','-preset','veryfast','-crf','27','-pix_fmt','yuv420p','-g','15','-keyint_min','15','-sc_threshold','0','-c:a','aac','-b:a','48k','-movflags','+faststart',target];
 const r=spawnSync(ffmpeg,args,{stdio:'inherit'});if(r.status!==0)process.exit(r.status||1);process.stdout.write(`Generated scene ${scene} ${high?'HD':'SD'}\n`);
}
for(const size of [192,512]){
 const s=size/192;const filters=[`drawbox=x=${33*s}:y=${65*s}:w=${91*s}:h=${63*s}:color=0xe87b58:t=fill`,`drawbox=x=${128*s}:y=${77*s}:w=${31*s}:h=${40*s}:color=0xe87b58:t=fill`,`drawbox=x=${58*s}:y=${78*s}:w=${36*s}:h=${36*s}:color=0x171719:t=fill`];
 const r=spawnSync(ffmpeg,['-hide_banner','-loglevel','error','-y','-f','lavfi','-i',`color=c=0x171719:s=${size}x${size}`,'-vf',filters.join(','),'-frames:v','1',resolve(root,`web/public/icon-${size}.png`)],{stdio:'inherit'});if(r.status!==0)process.exit(r.status||1);
}
