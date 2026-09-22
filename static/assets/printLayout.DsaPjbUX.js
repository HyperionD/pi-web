const a=t=>!Number.isFinite(t)||t<=0?0:Number(t.toFixed(3)),h=t=>`${a(t)}px`,g=t=>`${Number((a(t)/96).toFixed(4))}in`,s=t=>{const i=Number.parseFloat(t);return Number.isFinite(i)&&i>0?i:0},p=(t,i={})=>{const r=window.getComputedStyle(t),o=s(r.width)||t.offsetWidth||i.width||t.getBoundingClientRect().width,e=s(r.height)||s(r.minHeight)||t.offsetHeight||i.height||t.getBoundingClientRect().height;return{width:a(o),height:a(e)}},m=(t,i,r={})=>{const o=h(i.width),e=h(i.height),n=r.heightMode||"fixed";t.classList.add("viewer-print-page"),t.style.setProperty("--viewer-print-page-width",o),t.style.setProperty("--viewer-print-page-height",e),t.style.width=o,t.style.maxWidth="none",t.style.minHeight=e,n==="fixed"?(t.style.height=e,t.style.overflow="hidden"):(t.style.height="auto",t.style.overflow="visible")},f=({selector:t,width:i,height:r,heightMode:o="fixed"})=>{const e=h(i),n=h(r),d=o==="fixed"?`height:${n}!important;min-height:${n}!important;overflow:hidden!important;`:`height:auto!important;min-height:${n}!important;overflow:visible!important;`;return`
    @page { size: ${g(i)} ${g(r)}; margin: 0; }
    @media print {
      html, body {
        width: ${e};
        min-width: ${e};
        margin: 0 !important;
        padding: 0 !important;
        min-height: 0 !important;
        background: #ffffff !important;
      }
      .viewer-export-shell,
      .viewer-export-content {
        width: ${e} !important;
        min-height: 0 !important;
        margin: 0 !important;
        padding: 0 !important;
      }
      ${t} {
        width: ${e}!important;
        max-width: none!important;
        ${d}
        margin: 0!important;
        box-shadow: none!important;
        border: 0!important;
        break-after: page;
        page-break-after: always;
      }
      ${t}:last-child {
        break-after: auto;
        page-break-after: auto;
      }
    }
  `};export{m as a,f as b,h as f,p as g};
