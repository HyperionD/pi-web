import{R as W,ab as Z,W as R}from"./index.B6xlKfZm.js";import{a as $}from"./zoom.DyZhVRTZ.js";import{b as L,a as T,f as D}from"./printLayout.DsaPjbUX.js";import{p as V,a as I,d as N}from"./viewer.Dk1haoZJ.js";const F='<span class="msdoc-page-break"></span>',G='<p class="msdoc-paragraph"><br></p>',l={width:794,height:1123},z=.24,P=3,O=.15,j=`
.msdoc-stage{
  box-sizing:border-box;
  min-height:100%;
  padding:32px 24px 48px;
  background:var(--file-viewer-render-surface-background,#ececec);
  display:flex;
  flex-direction:column;
  align-items:center;
  gap:24px;
}
.msdoc-page{
  width:min(100%,794px);
  box-sizing:border-box;
}
.msdoc-page > .msdoc-root{
  box-sizing:border-box;
  width:100%;
  max-width:none;
  min-height:1123px;
  padding:clamp(24px,7%,96px) clamp(20px,6%,88px);
  background:#fff;
  border:1px solid #d9d9d9;
  box-shadow:0 1px 3px rgba(0,0,0,0.08), 0 12px 32px rgba(0,0,0,0.12);
  overflow-wrap:anywhere;
}
/* 表格不能沿用正文的 anywhere 断词，否则旧 DOC 的列宽和单元格内容会被过度压缩。 */
.msdoc-page .msdoc-table{
  display:table;
  width:auto;
  max-width:100%;
  table-layout:auto!important;
  border-collapse:collapse;
  border-spacing:0;
}
.msdoc-page .msdoc-table tbody,
.msdoc-page .msdoc-table tr{
  width:auto;
}
.msdoc-page .msdoc-cell{
  min-width:1.5em;
  padding:4px 6px;
  vertical-align:top;
  word-break:normal;
  overflow-wrap:normal;
  white-space:normal;
}
.msdoc-page .msdoc-cell .msdoc-paragraph{
  margin:0 0 4px;
  word-break:normal;
  overflow-wrap:break-word;
}
.msdoc-page .msdoc-cell .msdoc-paragraph:last-child{
  margin-bottom:0;
}
.msdoc-page .msdoc-cell span{
  white-space:normal!important;
}
.msdoc-page .msdoc-page-break{
  display:none;
}
[data-viewer-theme='dark'] .msdoc-stage{
  background:var(--file-viewer-render-surface-background,#111827);
}
[data-viewer-theme='dark'] .msdoc-page > .msdoc-root{
  color-scheme:dark;
  background:#161b22;
  border-color:rgba(139,148,158,.24);
  color:#e6edf3;
  box-shadow:0 18px 44px rgba(0,0,0,.36);
}
[data-viewer-theme='dark'] .msdoc-page > .msdoc-root span[style*='color:#000000']{
  color:#e6edf3!important;
}
[data-viewer-theme='dark'] .msdoc-page .msdoc-link{
  color:#58a6ff;
}
[data-viewer-theme='dark'] .msdoc-page .msdoc-attachment{
  border-color:rgba(139,148,158,.3);
  background:#21262d;
  color:#58a6ff;
}
@media (prefers-color-scheme:dark){
  [data-viewer-theme='system'] .msdoc-stage{
    background:var(--file-viewer-render-surface-background,#111827);
  }
  [data-viewer-theme='system'] .msdoc-page > .msdoc-root{
    color-scheme:dark;
    background:#161b22;
    border-color:rgba(139,148,158,.24);
    color:#e6edf3;
    box-shadow:0 18px 44px rgba(0,0,0,.36);
  }
  [data-viewer-theme='system'] .msdoc-page > .msdoc-root span[style*='color:#000000']{
    color:#e6edf3!important;
  }
  [data-viewer-theme='system'] .msdoc-page .msdoc-link{
    color:#58a6ff;
  }
  [data-viewer-theme='system'] .msdoc-page .msdoc-attachment{
    border-color:rgba(139,148,158,.3);
    background:#21262d;
    color:#58a6ff;
  }
}
@media (max-width: 860px){
  .msdoc-stage{
    padding:16px 12px 24px;
    gap:16px;
  }
  .msdoc-page{
    width:100%;
  }
  .msdoc-page > .msdoc-root{
    min-height:auto;
    padding:24px 20px;
    box-shadow:none;
  }
}
`,B=`
.msdoc-zoom-viewer{
  box-sizing:border-box;
  height:100%;
  overflow:auto;
  background:var(--file-viewer-render-surface-background,#ececec);
}
[data-viewer-theme='dark'] .msdoc-zoom-viewer{
  background:var(--file-viewer-render-surface-background,#111827);
}
@media (prefers-color-scheme:dark){
  [data-viewer-theme='system'] .msdoc-zoom-viewer{
    background:var(--file-viewer-render-surface-background,#111827);
  }
}
.msdoc-zoom-viewer .msdoc-stage{
  min-width:max-content;
}
.msdoc-zoom-viewer .msdoc-page{
  position:relative;
  max-width:none;
  overflow:visible;
}
.msdoc-zoom-viewer .msdoc-page > .msdoc-root{
  position:absolute;
  top:0;
  left:50%;
  width:${l.width}px;
  max-width:none;
  margin:0;
  transform-origin:top center;
}
`,K=a=>a.ownerDocument.defaultView;function X(a){return a.replace(/<(p|table|section)([^>]*?)style="([^"]*?\bbreak-before\s*:\s*page;?[^"]*?)"([^>]*)>/gi,r=>`${F}${r}`)}function U(a){return`<div class="msdoc-stage">${X(a).split(F).map(n=>`<section class="msdoc-page"><div class="msdoc-root">${n||G}</div></section>`).join("")}</div>`}function Y(a){const r=a.cloneNode(!0);return r.classList.remove("msdoc-zoom-viewer"),r.querySelectorAll("style[data-msdoc-zoom]").forEach(e=>e.remove()),r.querySelectorAll(".msdoc-stage, .msdoc-page, .msdoc-root").forEach(e=>{e.style.height="auto",e.style.maxHeight="none",e.style.overflow="visible",e.style.transform="none"}),r.querySelectorAll(".msdoc-page").forEach((e,n)=>{e.dataset.viewerPrintPageIndex=String(n),T(e,l,{heightMode:"min"}),e.style.position="relative",e.style.width=D(l.width),e.style.maxWidth="none",e.style.margin="0 auto 18px";const t=e.querySelector(".msdoc-root");t&&(t.style.position="relative",t.style.top="auto",t.style.left="auto",t.style.width=D(l.width),t.style.maxWidth="none",t.style.minHeight=D(l.height),t.style.height="auto",t.style.transform="none",t.style.transformOrigin="top left",t.style.boxShadow="none",t.style.border="0",t.style.overflow="visible")}),r.innerHTML}function q(){return L({selector:".viewer-export-content .msdoc-page",width:l.width,height:l.height,heightMode:"min"})}function J(a){const r=Array.from(a.querySelectorAll(".msdoc-page"));if(!r.length)return()=>{};a.classList.add("msdoc-zoom-viewer");const e=K(a),n=e?.ResizeObserver,t=a.ownerDocument.createElement("style");t.dataset.msdocZoom="true",t.textContent=B,a.prepend(t);const w=$();let m=0,h=1,s=1,c=1;const y=o=>Math.min(P,Math.max(z,Number(o.toFixed(2)))),p=()=>{let o=s,i=c,u=!1;r.forEach(f=>{const g=f.querySelector(".msdoc-root");if(!g)return;const M=g.offsetWidth||l.width,E=Math.max(g.scrollHeight,g.offsetHeight,l.height),H=Math.max(a.clientWidth-48,120),C=Math.min(1,Math.max(z,H/M)),A=y(C*h);u||(o=A,i=C,u=!0),g.style.transform=`translateX(-50%) scale(${A})`,f.style.width=`${Math.ceil(Math.max(M*A,120))}px`,f.style.height=`${Math.ceil(E*A)}px`}),u&&(s=o,c=i,w.emit())},x=()=>{if(!e){p();return}e.cancelAnimationFrame(m),m=e.requestAnimationFrame(()=>{p()})},b=()=>({scale:s,label:`${Math.round(s*100)}%`,canZoomIn:s<P,canZoomOut:s>z,canReset:h!==1,minScale:z,maxScale:P}),v=o=>(h=Math.min(6,Math.max(.2,Number(o.toFixed(2)))),e?.cancelAnimationFrame(m),p(),b()),S=o=>v(o/Math.max(c,.01)),k=()=>{var o;const i=(o=r[0])===null||o===void 0?void 0:o.querySelector(".msdoc-root");return{width:i?.offsetWidth||l.width,height:l.height}},_=o=>{var i,u;const f=k(),g=o.mode==="auto"?"width":o.mode,M=R({mode:g,viewportWidth:Math.max(1,o.viewportWidth||a.clientWidth||0),viewportHeight:Math.max(1,o.viewportHeight||a.clientHeight||0),contentWidth:f.width,contentHeight:f.height,minScale:(i=o.minScale)!==null&&i!==void 0?i:z,maxScale:(u=o.maxScale)!==null&&u!==void 0?u:P});if(!M)return{applied:!1,mode:o.mode,resize:o.resize,source:o.source,reason:"unmeasurable",provider:"zoom"};const E=S(M);return{applied:!0,mode:o.mode,resize:o.resize,scale:E.scale,source:o.source,provider:"zoom"}};a.dataset.viewerZoomProvider="doc",W(a,{zoomIn:()=>v((s+O)/Math.max(c,.01)),zoomOut:()=>v((s-O)/Math.max(c,.01)),resetZoom:()=>v(1),setZoom:S,fit:_,getState:b,subscribe:w.subscribe});const d=n?new n(x):null;return d?.observe(a),r.forEach(o=>{const i=o.querySelector(".msdoc-root");i&&d?.observe(i)}),p(),()=>{e?.cancelAnimationFrame(m),d?.disconnect(),Z(a),t.remove(),a.classList.remove("msdoc-zoom-viewer")}}async function ae(a,r,e){var n,t,w,m,h,s,c,y,p,x,b;const v=await V(a,{renderOptions:{reviewMode:(w=(t=(n=e?.options)===null||n===void 0?void 0:n.docx)===null||t===void 0?void 0:t.reviewMode)!==null&&w!==void 0?w:"all",css:`${N()}
${j}`,externalLinkPolicy:(s=(h=(m=e?.options)===null||m===void 0?void 0:m.docx)===null||h===void 0?void 0:h.externalLinkPolicy)!==null&&s!==void 0?s:"block",externalResourcePolicy:(p=(y=(c=e?.options)===null||c===void 0?void 0:c.docx)===null||y===void 0?void 0:y.externalResourcePolicy)!==null&&p!==void 0?p:"block"}}),S=r.ownerDocument.defaultView;if(!S)throw new Error("The DOC target must belong to a browser document");const k=r.ownerDocument.createElement("style");k.dataset.msdoc="",k.textContent=v.css;const _=r.ownerDocument.createElement("div");_.append(I(U(v.html),S)),r.replaceChildren(k,...Array.from(_.childNodes));const d=J(r);return(x=e?.registerExportAdapter)===null||x===void 0||x.call(e,{includeDocumentStyles:!1,getPrintMaskPages:()=>Array.from(r.querySelectorAll(".msdoc-page")),printStyle:q,toHtml:()=>Y(r)}),(b=e?.registerThumbnailAdapter)===null||b===void 0||b.call(e,{getTarget:()=>r.querySelector(".msdoc-page")||r}),{$el:r,unmount(){var o,i;(o=e?.registerExportAdapter)===null||o===void 0||o.call(e,null),(i=e?.registerThumbnailAdapter)===null||i===void 0||i.call(e,null),d(),r.replaceChildren()}}}export{ae as default};
