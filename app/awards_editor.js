'use strict';
// Malowar Awards v1.3.9 - part of the main news editor, not a standalone application.
// Approved badge PNGs are used without re-rendering; canvas is a compositor only.
let AWARD_KINDS = ['pearl','world','doubt'];
let AWARD_NAMES = {pearl:'Скрытая жемчужина',world:'Живой мир',doubt:'Сомнительно'};
const AWARD_RATIO = 219 / 1433;
let coverEdit = null;
const clamp01 = v => Math.max(0,Math.min(1,v));
function coverAwardHeight(layer){return layer.width * AWARD_RATIO * 1280 / 720}
function layerRect(layer){return {x:layer.x*1280,y:layer.y*720,w:layer.width*1280,h:layer.width*1280*AWARD_RATIO}}
function heroRect(){
  if(!coverEdit?.hero)return null;
  const im=coverEdit.hero, a=coverEdit.layout;
  const w=Math.min(a.hero_width*1280,720*0.9*im.naturalWidth/im.naturalHeight);
  const h=w*im.naturalHeight/im.naturalWidth;
  return {x:a.hero_x*1280,y:a.hero_y*720,w,h};
}
function coverDrawImage(ctx,img){
  const w=1280,h=720,scale=Math.max(w/img.naturalWidth,h/img.naturalHeight);
  const sw=w/scale,sh=h/scale;
  ctx.drawImage(img,(img.naturalWidth-sw)/2,(img.naturalHeight-sh)/2,sw,sh,0,0,w,h);
}
function drawCoverEditor(withGuides=true){
  const ed=coverEdit;if(!ed)return;
  const canvas=el('awardsCanvas'),ctx=canvas.getContext('2d');
  ctx.clearRect(0,0,1280,720);
  ctx.save();if(ed.layout.mirror_cover){ctx.translate(1280,0);ctx.scale(-1,1)}
  coverDrawImage(ctx,ed.cover);ctx.restore();
  if(ed.hero){const r=heroRect();ctx.save();if(ed.layout.mirror_hero){ctx.translate(r.x*2+r.w,0);ctx.scale(-1,1)}ctx.drawImage(ed.hero,r.x,r.y,r.w,r.h);ctx.restore()}
  for(const layer of ed.layout.layers){
    const im=ed.badges[layer.kind],r=layerRect(layer);if(im)ctx.drawImage(im,r.x,r.y,r.w,r.h);
    if(withGuides&&ed.selected===layer.kind){ctx.save();ctx.lineWidth=3;ctx.strokeStyle='#fceab2';ctx.setLineDash([9,5]);ctx.strokeRect(r.x,r.y,r.w,r.h);ctx.restore()}
  }
  if(withGuides&&ed.selected==='hero'&&ed.hero){const r=heroRect();ctx.save();ctx.lineWidth=3;ctx.strokeStyle='#64efd6';ctx.setLineDash([10,5]);ctx.strokeRect(r.x,r.y,r.w,r.h);ctx.restore()}
  if(withGuides){
    ctx.save();ctx.fillStyle='#dcecff';ctx.font='20px Segoe UI';ctx.shadowColor='#06101bee';ctx.shadowBlur=7;
    ctx.fillText('Перетаскивай плашки мышью • контуры исчезнут после сохранения',25,700);
    ctx.restore();
  }
  const overlaps=[];
  // The user chooses important composition zones; if a separate hero exists, warn
  // about potential overlap. Baked-in faces require visual review.
  const h=heroRect();if(h){for(const l of ed.layout.layers){const r=layerRect(l);if(r.x<h.x+h.w&&r.x+r.w>h.x&&r.y<h.y+h.h&&r.y+r.h>h.y){overlaps.push(AWARD_NAMES[l.kind])}}}
  el('awardsWarning').textContent=overlaps.length?'Внимание: награды перекрывают отдельный слой персонажа: '+overlaps.join(', '):'Проверь лица, логотипы и важные надписи перед сохранением.';
}
function autoAwardLayout(){
  if(!coverEdit)return;const ed=coverEdit,items=ed.layout.layers,n=items.length;
  const gap=Number(el('awardsGap').value),right=36,vert=36;
  items.forEach((a,i)=>{
    const w=a.width*1280,h=w*AWARD_RATIO;
    a.x=(1280-right-w)/1280;
    a.y=(n===1?720-h-vert:n===2?(720-(2*h+gap))/2+i*(h+gap):(i===0?vert:i===1?(720-h)/2:720-h-vert))/720;
    a.x=clamp01(a.x);a.y=clamp01(a.y);
  });
  drawCoverEditor();
}
function syncAwardEditorControls(){
  if(!coverEdit)return;
  const ed=coverEdit,l=ed.layout;
  for(const k of AWARD_KINDS)el('awardCheck_'+k).checked=l.layers.some(x=>x.kind===k);
  el('awardMirrorCover').checked=!!l.mirror_cover;
  el('awardMirrorHero').checked=!!l.mirror_hero;
  el('heroScale').value=Math.round(l.hero_width*100);
  el('awardSelected').textContent=ed.selected==='hero'?'Персонаж':AWARD_NAMES[ed.selected]||'Выбери награду на обложке';
  const selected=l.layers.find(x=>x.kind===ed.selected);
  el('awardScale').disabled=!selected;
  if(selected)el('awardScale').value=Math.round(selected.width*100);
  el('awardsHeroStatus').textContent=ed.hero?'Отдельный слой персонажа загружен. Можно перетаскивать и отражать.':'Дополнительный PNG персонажа не выбран. Для фотографии с уже нарисованным персонажем используй отражение всей обложки.';
}
function setAwardSelection(kind){if(!coverEdit)return;coverEdit.selected=kind;syncAwardEditorControls();drawCoverEditor()}
function awardToggle(kind,checked){
  if(!coverEdit)return;const layers=coverEdit.layout.layers;
  if(checked&&!layers.some(x=>x.kind===kind)){if(layers.length>=3){notify('На обложке максимум три награды',true);syncAwardEditorControls();return}layers.push({kind,x:0.53,y:0.8,width:.43})}
  if(!checked)coverEdit.layout.layers=layers.filter(x=>x.kind!==kind);
  if(checked)coverEdit.selected=kind;
  else if(coverEdit.selected===kind)coverEdit.selected=coverEdit.layout.layers[0]?.kind||'';
  autoAwardLayout();syncAwardEditorControls();
}
function browserFileImage(blob){return new Promise((resolve,reject)=>{
  const url=URL.createObjectURL(blob),im=new Image();im.onload=()=>{if(coverEdit)coverEdit.urls.push(url);else URL.revokeObjectURL(url);resolve(im)};
  im.onerror=()=>{URL.revokeObjectURL(url);reject(Error('Не удалось прочитать изображение'))};im.src=url;
})}
async function coverImageFromAPI(url){
  const response=await fetch(url,{headers:{'X-MNA-Key':KEY}});
  if(!response.ok)throw Error('Ошибка загрузки исходной обложки');
  return browserFileImage(await response.blob());
}
async function openAwardsEditor(){
  const it=current();if(!it||!it.images?.length)return;
  if(dirty){notify('Сначала сохрани правки текста новости',true);return}
  if(coverEdit){notify('Редактор уже открыт');return}
  const layout=it.awards?structuredClone(it.awards):{mirror_cover:false,mirror_hero:false,hero_x:0.035,hero_y:0.04,hero_width:0.42,layers:[]};
  coverEdit={id:it.id,cover:null,hero:null,badges:{},layout,selected:layout.layers[0]?.kind||'',urls:[],drag:null,heroFile:null,clearHero:false};
  const ed=coverEdit;refreshAwardCatalogUI();el('awardsOverlay').classList.add('open');
  el('awardsCanvas').style.pointerEvents='none';el('awardsWarning').textContent='Загружаю исходную обложку…';
  try{
    const loadImages=AWARD_KINDS.map(async k=>{
      const resp=await fetch('/assets/awards/'+k+'.png');if(!resp.ok)throw Error('Награда '+k+' не найдена');ed.badges[k]=await browserFileImage(await resp.blob());
    });
    await Promise.all([coverImageFromAPI('/api/cover-original?id='+encodeURIComponent(it.id)).then(img=>ed.cover=img),...loadImages]);
    if(it.awards_hero){try{ed.hero=await coverImageFromAPI('/api/cover-hero?id='+encodeURIComponent(it.id))}catch(e){notify('Слой персонажа не найден: '+e.message,true)}}
    if(coverEdit!==ed)return;
    if(!it.awards)autoAwardLayout();syncAwardEditorControls();drawCoverEditor();
    el('awardsCanvas').style.pointerEvents='auto';
  }catch(e){closeAwardsEditor();notify(e.message,true)}
}
function closeAwardsEditor(){if(coverEdit){for(const u of coverEdit.urls)URL.revokeObjectURL(u)}coverEdit=null;el('awardsHeroFile').value='';el('awardsOverlay').classList.remove('open')}
function coverCanvasPoint(e){const r=el('awardsCanvas').getBoundingClientRect();return {x:(e.clientX-r.left)*1280/r.width,y:(e.clientY-r.top)*720/r.height}}
function hitAward(point){
  if(!coverEdit)return null;
  for(const l of [...coverEdit.layout.layers].reverse()){const r=layerRect(l);if(point.x>=r.x&&point.x<=r.x+r.w&&point.y>=r.y&&point.y<=r.y+r.h)return l.kind}
  const r=heroRect();if(r&&point.x>=r.x&&point.x<=r.x+r.w&&point.y>=r.y&&point.y<=r.y+r.h)return 'hero';
  return null;
}
const awardsCanvas=el('awardsCanvas');
awardsCanvas.addEventListener('pointerdown',e=>{if(!coverEdit?.cover)return;const p=coverCanvasPoint(e),hit=hitAward(p);if(!hit)return;setAwardSelection(hit);
  let rect=hit==='hero'?heroRect():layerRect(coverEdit.layout.layers.find(l=>l.kind===hit));
  coverEdit.drag={kind:hit,dx:p.x-rect.x,dy:p.y-rect.y};awardsCanvas.setPointerCapture(e.pointerId);
});
awardsCanvas.addEventListener('pointermove',e=>{
  if(!coverEdit?.drag)return;const p=coverCanvasPoint(e),d=coverEdit.drag;
  if(d.kind==='hero'){
    const r=heroRect();coverEdit.layout.hero_x=Math.max(0,Math.min(1-r.w/1280,(p.x-d.dx)/1280));coverEdit.layout.hero_y=Math.max(0,Math.min(1-r.h/720,(p.y-d.dy)/720));
  }else {const l=coverEdit.layout.layers.find(x=>x.kind===d.kind);if(!l)return;
    l.x=Math.max(0,Math.min(1-l.width,(p.x-d.dx)/1280));l.y=Math.max(0,Math.min(1-coverAwardHeight(l),(p.y-d.dy)/720));
  } drawCoverEditor();
});
for(const type of ['pointerup','pointercancel','lostpointercapture'])awardsCanvas.addEventListener(type,()=>{if(coverEdit)coverEdit.drag=null});
el('awardsAuto').onclick=()=>{autoAwardLayout();notify('Награды выстроены автоматически. Теперь можешь перетащить их вручную.')};
el('awardMirrorCover').onchange=e=>{if(!coverEdit)return;coverEdit.layout.mirror_cover=e.target.checked;drawCoverEditor()};
el('awardMirrorHero').onchange=e=>{if(!coverEdit)return;coverEdit.layout.mirror_hero=e.target.checked;drawCoverEditor()};
el('awardScale').oninput=e=>{const l=coverEdit?.layout.layers.find(x=>x.kind===coverEdit.selected);if(!l)return;l.width=Number(e.target.value)/100;l.x=Math.min(l.x,1-l.width);l.y=Math.min(l.y,1-coverAwardHeight(l));drawCoverEditor()};
el('heroScale').oninput=e=>{if(!coverEdit)return;coverEdit.layout.hero_width=Number(e.target.value)/100;const r=heroRect();if(r){coverEdit.layout.hero_x=Math.min(coverEdit.layout.hero_x,1-r.w/1280);coverEdit.layout.hero_y=Math.min(coverEdit.layout.hero_y,1-r.h/720)}drawCoverEditor()};
el('awardsHeroFile').onchange=async e=>{const file=e.target.files?.[0];if(!file||!coverEdit)return;
  if(file.type!=='image/png'||file.size>12*1024*1024){notify('Нужен PNG с прозрачным фоном до 12 МБ',true);return}
  try{coverEdit.hero=await browserFileImage(file);coverEdit.heroFile=file;coverEdit.clearHero=false;coverEdit.layout.hero_x=.035;coverEdit.layout.hero_y=.04;coverEdit.selected='hero';syncAwardEditorControls();drawCoverEditor()}catch(err){notify(err.message,true)}
};
el('awardsRemoveHero').onclick=()=>{if(!coverEdit)return;coverEdit.hero=null;coverEdit.heroFile=null;coverEdit.clearHero=true;coverEdit.selected=coverEdit.layout.layers[0]?.kind||'';syncAwardEditorControls();drawCoverEditor()};
el('awardsClose').onclick=closeAwardsEditor;
el('awardsReset').onclick=async()=>{
  if(!coverEdit||!confirm('Восстановить исходную обложку и убрать все награды из этой новости?'))return;
  try{await api('cover-awards-reset','POST',{id:coverEdit.id});closeAwardsEditor();lastEditorSig='';notify('Исходная обложка восстановлена');await loadState()}catch(e){notify(e.message,true)}
};
el('awardsSave').onclick=async()=>{
  if(!coverEdit?.cover)return;const ed=coverEdit;
  // Verify images are inside the image bounds before sending.
  for(const l of ed.layout.layers){if(l.x<0||l.y<0||l.x+l.width>1.00001||l.y+coverAwardHeight(l)>1.00001){notify('Плашка выходит за край. Передвинь её.',true);return}}
  const button=el('awardsSave');button.disabled=true;
  try{
    drawCoverEditor(false);
    const png=await new Promise(resolve=>el('awardsCanvas').toBlob(resolve,'image/png'));
    if(!png||png.size>12*1024*1024)throw Error('PNG слишком большой (максимум 12 МБ)');
    const fd=new FormData();fd.append('id',ed.id);fd.append('layout',JSON.stringify(ed.layout));fd.append('cover',png,'Malowar_Awards_Cover.png');
    if(ed.heroFile)fd.append('hero',ed.heroFile,'Malowar_Hero.png');
    if(ed.clearHero)fd.append('clear_hero','1');
    await api('cover-awards','POST',fd);
    closeAwardsEditor();lastEditorSig='';notify('Награды сохранены на обложке. Одобрение сброшено: проверь изображение заново.');await loadState();
  }catch(e){notify(e.message,true)}finally{button.disabled=false;if(coverEdit===ed)drawCoverEditor()}
};

function refreshAwardCatalogUI(){
 const catalog=s?.award_catalog||[];
 const ready=catalog.filter(x=>x.status==='ready');
 AWARD_KINDS=ready.map(x=>x.id);AWARD_NAMES=Object.fromEntries(catalog.map(x=>[x.id,x.name]));
 const r=el('awardReadyList');r.replaceChildren();
 for(const a of ready){const label=elem('label','','');const cb=document.createElement('input');cb.type='checkbox';cb.id='awardCheck_'+a.id;cb.onchange=()=>awardToggle(a.id,cb.checked);label.append(cb,document.createTextNode(' '+a.name));r.append(label)}
 const candidates=el('awardCandidateList');candidates.replaceChildren();
 for(const a of catalog.filter(x=>x.status==='candidate')){
  const line=elem('div','small','');line.style.cssText='margin:6px 0;border-bottom:1px solid #344052;padding-bottom:5px';
  const text=elem('span','',a.name+' · '+a.category);const file=document.createElement('input');file.type='file';file.accept='image/png';file.style.display='none';
  file.onchange=()=>{if(file.files?.[0])awardUploadAsset(a.id,file.files[0])};
  const btn=elem('button','','Добавить PNG');btn.style.cssText='padding:4px 8px;margin-left:8px';btn.onclick=()=>file.click();line.append(text,btn,file);candidates.append(line)
 }
}
async function awardUploadAsset(id,file){
 if(!file||file.type!=='image/png'){notify('Нужен PNG',true);return}
 try {const fd=new FormData();fd.append('id',id);fd.append('image',file,file.name);await api('award-upload','POST',fd);notify('Графика добавлена в каталог; открой редактор повторно для выбора награды.');closeAwardsEditor();await loadState()}
 catch(e){notify(e.message,true)}
}
el('newAwardAdd').onclick=async()=>{const name=el('newAwardName').value.trim();if(!name)return;try{await api('award-add','POST',{name,category:'Редакция'});el('newAwardName').value='';await loadState();refreshAwardCatalogUI();notify('Достижение добавлено в очередь дизайна')}catch(e){notify(e.message,true)}};
