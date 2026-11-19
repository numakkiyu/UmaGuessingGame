import fragment from "./match-curtain.frag?raw";
const clamp = (value: number) => Math.max(0, Math.min(1, value));
export const smooth = (value: number) => { const x=clamp(value); return x*x*x*(x*(x*6-15)+10); };

// Each location responds to the same clock, with travel time measured from the center.
export function openingAt(x: number, progress: number, width: number, height: number) {
  const nx=Math.abs(x-width/2)/(width/2),distance=nx*nx*(3-2*nx);
  const release=(progress-.16)/.84;
  const local=smooth((release-.06*distance)/.94),edge=smooth((release-.06)/.94);
  const gap=(height/2+4)*local;
  const breath=Math.sin(Math.PI*local)*Math.sin(Math.PI*distance)*Math.min(5,height*.008);
  const film=(1+Math.min(18,height*.035)*smooth(progress/.16))*(1-smooth((progress-.58)/.42));
  return {gap,top:gap+breath,bottom:gap-breath*.45,lift:(height/2+4)*edge,film,spread:1};
}
const line = (points: number[][]) => points.map(([x,y],i)=>`${i?'L':'M'}${x!.toFixed(2)} ${y!.toFixed(2)}`).join(' ');

export class CenterWave {
  panels: HTMLElement[]; paths: SVGPathElement[]; progress: number; w=0; h=0;
  constructor(panels: HTMLElement[], paths: SVGPathElement[]) { this.panels=panels; this.paths=paths; this.progress=0; }
  resize(w: number,h: number) { this.w=w;this.h=h;this.paint(this.progress); }
  paint(progress: number) {
    this.progress=progress;
    const w=this.w,h=this.h;if(!w||!h)return;
    const cy=h/2,samples=80;
    const upper: number[][]=[],lower: number[][]=[];
    const lift=openingAt(0,progress,w,h).lift;
    for(let i=0;i<=samples;i++) {
      const x=i/samples*w,field=openingAt(x,progress,w,h);
      upper.push([x,cy-field.top]);lower.push([x,cy+field.bottom]);
    }
    this.panels.forEach((panel,index)=>{
      const top=index<2,right=index%2===1,points=[];
      for(let i=0;i<=samples/2;i++) {
        const x=(right?w/2:0)+i/samples*w,f=openingAt(x,progress,w,h);
        const cut=top?100-(f.top-lift)/cy*100:(f.bottom-lift)/cy*100;
        points.push(`${(i/(samples/2)*100).toFixed(3)}% ${cut.toFixed(3)}%`);
      }
      panel.style.clipPath=top?`polygon(0 0,100% 0,${points.reverse().join(',')})`:`polygon(${points.join(',')},100% 100%,0 100%)`;
      panel.style.transform=`translateY(${top?-lift:lift}px)`;
    });
    // Nested films follow the exact cut. They give a short feather, without a blur filter.
    this.paths.forEach((path,index)=>{
      const feather=[1,1.8,2.8,4.1][index]!,outsideTop=[],outsideBottom=[];
      for(let i=0;i<=samples;i++) {
        const x=i/samples*w,f=openingAt(x,progress,w,h),thickness=f.film*feather;
        outsideTop.push([x,cy-f.top-thickness]);outsideBottom.push([x,cy+f.bottom+thickness]);
      }
      const t=line(upper),b=line(lower);
      path.setAttribute('d',`${t} ${line(outsideTop.reverse()).replace(/^M/,'L')} Z ${b} ${line(outsideBottom.reverse()).replace(/^M/,'L')} Z`);
    });

  }
}

// Both halves sample this same global contour, including across the quadrant join.
export function closingInset(_y: number, _progress: number, _w: number, _h: number, _side: number) { return 0; }
export class ClosingWave {
  panels: HTMLElement[]; progress: number; w=0; h=0;
  constructor(panels: HTMLElement[]){this.panels=panels;this.progress=0;}
  resize(w: number,h: number){this.w=w;this.h=h;}
  paint(progress: number){
    this.progress=progress;const w=this.w,h=this.h,cx=w/2,cy=h/2,base=smooth(progress);
    if(!w||!h)return;
    this.panels.forEach((panel,index)=>{
      const left=index%2===0,top=index<2,start=top?0:cy,points=[];
      for(let i=0;i<=52;i++){
        const y=start+i/52*cy,inset=closingInset(y,progress,w,h,left?0:1)/cx*100;
        points.push(`${(left?100-inset:inset).toFixed(3)}% ${(i/52*100).toFixed(3)}%`);
      }
      panel.style.clipPath=left?`polygon(0 0,${points.join(',')},0 100%)`:`polygon(${points.join(',')},100% 100%,100% 0)`;
      panel.style.transform=`translateX(${(left?-1:1)*cx*(1-base)}px)`;
    });
  }
}

export class WhiteRoute {
  svg: SVGSVGElement; paths: SVGPathElement[]; progress: number; leg=0; lengths: number[]=[]; duration=3400;
  constructor(svg: SVGSVGElement) { this.svg=svg;this.paths=[...svg.querySelectorAll('path')];this.progress=0; }
  resize(w: number,h: number) {
    this.svg.setAttribute('viewBox',`0 0 ${w} ${h}`);
    const cx=w/2,cy=h/2,pad=0,left=0,right=w,top=0,bottom=h;
    const down=`M ${cx} ${cy} V ${bottom} H ${left} V ${top} H ${right} V ${bottom} H ${cx}`;
    const up=`M ${cx} ${cy} V ${top} H ${right} V ${bottom} H ${left} V ${top} H ${cx}`;
    this.paths.forEach((path,index)=>path.setAttribute('d',index?up:down));
    this.leg=cy-pad;this.lengths=this.paths.map(path=>path.getTotalLength());
    this.duration=Math.max(3400,Math.min(4200,2800+(w+h)*.55));
    this.paint(this.progress);
  }
  paint(progress: number) {
    this.progress=progress;if(!this.lengths)return;
    const travel=smooth(progress/.84),fade=smooth((progress-.84)/.16);
    this.svg.style.opacity=String(1-fade);this.svg.dataset.progress=progress.toFixed(4);
    this.paths.forEach((path,index)=>{
      const total=this.lengths[index]!,distance=(total-this.leg)*travel;
      // The seam feeds a growing border: consumed center length remains at the rim.
      const t=Math.min(1,distance/(2*this.leg));
      const tail=2*this.leg*(t-t**6+3*t**5-2.5*t**4);
      const head=distance+this.leg;
      path.style.strokeDasharray=`${head-tail} ${total+4}`;
      path.style.strokeDashoffset=String(-tail);
      path.dataset.tail=tail.toFixed(3);path.dataset.head=head.toFixed(3);
    });
  }
  hide(){this.svg.style.opacity='0';}
}

const vertex=`attribute vec2 position;void main(){gl_Position=vec4(position,0.,1.);}`;

export class EdgeField {
  canvas: HTMLCanvasElement; gl: WebGLRenderingContext | null=null; ctx: CanvasRenderingContext2D | null=null;
  uniforms: Record<string, WebGLUniformLocation | null>={};
  alpha: number; progress: number; closing: number; forming: number; returning: number; returnPhase: number; flowOffset: number; frame: number; epoch: number;
  w=0; h=0; ratio=1; failure=0;
  private boundSignal: AbortSignal | undefined;
  private disposeGL: (()=>void) | undefined;
  constructor(canvas: HTMLCanvasElement, fallback=false) {
    this.canvas=canvas;this.alpha=0;this.progress=-1;this.closing=-1;this.forming=-1;this.returning=-1;this.returnPhase=0;this.flowOffset=0;this.frame=0;this.epoch=performance.now();
    try {
      if(fallback)throw Error('Requested 2D preview');
      const gl=canvas.getContext('webgl',{alpha:true,antialias:false,premultipliedAlpha:false,preserveDrawingBuffer:false});
      if(!gl)throw Error('WebGL unavailable');
      const shader=(type: number,source: string)=>{const s=gl.createShader(type);if(!s)throw Error('Shader allocation failed');gl.shaderSource(s,source);gl.compileShader(s);if(!gl.getShaderParameter(s,gl.COMPILE_STATUS))throw Error(gl.getShaderInfoLog(s) ?? 'Shader compilation failed');return s;};
      const program=gl.createProgram();if(!program)throw Error('Program allocation failed');gl.attachShader(program,shader(gl.VERTEX_SHADER,vertex));gl.attachShader(program,shader(gl.FRAGMENT_SHADER,fragment));gl.linkProgram(program);
      if(!gl.getProgramParameter(program,gl.LINK_STATUS))throw Error(gl.getProgramInfoLog(program) ?? 'Program linking failed');
      gl.useProgram(program);const buffer=gl.createBuffer();gl.bindBuffer(gl.ARRAY_BUFFER,buffer);gl.bufferData(gl.ARRAY_BUFFER,new Float32Array([-1,-1,1,-1,-1,1,1,1]),gl.STATIC_DRAW);
      const pos=gl.getAttribLocation(program,'position');gl.enableVertexAttribArray(pos);gl.vertexAttribPointer(pos,2,gl.FLOAT,false,0,0);
      this.gl=gl;this.uniforms=Object.fromEntries(['size','resolution','time','amount','progress','closing','forming','returning','returnPhase','flowOffset','failure'].map(name=>[name,gl.getUniformLocation(program,name)]));
      canvas.dataset.renderer='webgl';
      this.disposeGL=()=>{gl.deleteBuffer(buffer);for(const shader of gl.getAttachedShaders(program) ?? [])gl.deleteShader(shader);gl.deleteProgram(program);};
    } catch {
      // A canvas with an allocated GL context cannot subsequently become a 2D canvas.
      const replacement=canvas.cloneNode() as HTMLCanvasElement;canvas.replaceWith(replacement);this.canvas=replacement;this.ctx=replacement.getContext('2d');replacement.dataset.renderer='2d';
    }
    this.canvas.addEventListener('webglcontextlost',e=>{e.preventDefault();this.gl=null;this.canvas.dataset.renderer='static';this.canvas.dispatchEvent(new Event('rendererchange'));this.canvas.style.background='linear-gradient(90deg,#ffffff30,transparent 5%,transparent 95%,#ffffff30)';});
  }
  resize(w: number,h: number) {
    this.w=w;this.h=h;const ratio=Math.min(1.5,devicePixelRatio||1);
    this.canvas.width=Math.round(w*ratio);this.canvas.height=Math.round(h*ratio);this.ratio=ratio;this.render(performance.now());
  }
  render(now: number,still=false) {
    if(!this.w||!this.h)return;
    const t=still?0:(now-this.epoch)/1000;
    if(this.gl) {
      const gl=this.gl;gl.viewport(0,0,this.canvas.width,this.canvas.height);gl.uniform2f(this.uniforms.size!,this.w,this.h);gl.uniform2f(this.uniforms.resolution!,this.canvas.width,this.canvas.height);gl.uniform1f(this.uniforms.progress!,this.progress);gl.uniform1f(this.uniforms.closing!,this.closing);gl.uniform1f(this.uniforms.forming!,this.forming);gl.uniform1f(this.uniforms.returning!,this.returning);gl.uniform1f(this.uniforms.returnPhase!,this.returnPhase);gl.uniform1f(this.uniforms.flowOffset!,this.flowOffset);gl.uniform1f(this.uniforms.failure!,this.failure);gl.uniform1f(this.uniforms.time!,t);gl.uniform1f(this.uniforms.amount!,this.alpha);gl.drawArrays(gl.TRIANGLE_STRIP,0,4);
    } else if(this.ctx) {
      const c=this.ctx,w=this.w,h=this.h;c.setTransform(this.ratio,0,0,this.ratio,0,0);c.clearRect(0,0,w,h);
      const perimeter=2*(w+h),colorIn=this.forming>=0?smooth((this.forming-.84)/.16):1;
      const travel=this.forming>=0?smooth(this.forming/.84):1,mod=(x: number)=>(x%1+1)%1;
      const phase=-t*.105-Math.sin(t*.64)*.012+this.flowOffset;
      const palette=(s: number)=>[0,.33,.67].map(offset=>Math.round(255*(.57+.43*Math.cos(2*Math.PI*(s*2+t*.085+offset)))));
      for(const [side,length] of [[0,w],[1,h],[2,w],[3,h]] as [number, number][]) {
        c.save();if(side===1){c.translate(w,0);c.rotate(Math.PI/2);}if(side===2){c.translate(w,h);c.rotate(Math.PI);}if(side===3){c.translate(0,h);c.rotate(-Math.PI/2);}
        const count=Math.ceil(length/8);
        for(let i=0;i<count;i++){
          const x=(i+.5)/count*length;
          const s=mod([w/2-x,w/2+h+w+h-x,w/2+h+w-x,w/2+h-x][side]!/perimeter);
          const age=travel-Math.min(mod(-s),mod(.5-s)),visited=smooth(age*perimeter/9);
          const bloom=this.forming>=0?smooth((this.forming-.18)/.62)*smooth(age/.20):1;
          const amount=this.progress>=0?0:this.forming>=0?visited+(1-visited)*colorIn:this.alpha*(this.returning>=0?1-smooth((this.returning-.38)/.44):1);
          const energy=(.5+.5*Math.cos(2*Math.PI*(s-phase)))**8+.9*(.5+.5*Math.cos(2*Math.PI*(s-phase-.5)))**11;
          const depth=(8+Math.min(w,h)*(.034+.05*energy))*.68;
          const rgb=palette(s).map((v,index)=>Math.round((255+(v-255)*colorIn)*(1-this.failure*.9)+[255,31,23][index]!*this.failure*.9));
          const gradient=c.createLinearGradient(0,0,0,depth*4);
          for(const f of [0,.003,.01,.025,.1,.25,.5,.75,1]){const distance=f*depth*4,core=1-smooth((distance-.65)/.8),haze=Math.exp(-((f*4)**1.28));gradient.addColorStop(f,`rgba(${rgb.join(',')},${(core+(haze-core)*bloom)*amount})`);}
          const x0=Math.round(i/count*length*this.ratio)/this.ratio,x1=Math.round((i+1)/count*length*this.ratio)/this.ratio;
          c.fillStyle=gradient;c.fillRect(x0,0,x1-x0,depth*4);
        }
        c.restore();
      }
      if(this.returning>=0){
        const reach=smooth((this.returning-.34)/.56),settle=smooth((this.returning-.64)/.36);
        const thickness=17+(1-17)*settle,rise=smooth((this.returning-.30)/.14)*smooth((this.returning-.46)/.48);
        const gradient=c.createLinearGradient(0,h/2-thickness*4,0,h/2+thickness*4);
        for(const f of [0,.25,.45,.48,.49,.5,.51,.52,.55,.75,1]){
          const dy=Math.abs(f-.5)*thickness*8,core=1-smooth((dy-.45)/.7),haze=Math.exp(-((dy/thickness)**1.5))*(1-settle)*.86;
          gradient.addColorStop(f,`rgba(255,255,255,${Math.max(core,haze)*rise})`);
        }
        c.fillStyle=gradient;c.fillRect(0,h/2-thickness*4,w/2*reach,thickness*8);c.fillRect(w-w/2*reach,h/2-thickness*4,w/2*reach,thickness*8);
      }
      if(this.forming>=0){
        const distance=perimeter*travel,a=Math.min(1,distance/h),tail=h*(a-a**6+3*a**5-2.5*a**4);
        const opacity=1-smooth((tail-h*.46)/(h*.04));
        if(opacity>0){
          const g=c.createLinearGradient(w/2-1,0,w/2+1,0);g.addColorStop(0,'#ffffff00');g.addColorStop(.5,`rgba(255,255,255,${opacity})`);g.addColorStop(1,'#ffffff00');c.fillStyle=g;
          c.fillRect(w/2-1,0,2,Math.max(0,h/2-tail));c.fillRect(w/2-1,h/2+tail,2,Math.max(0,h/2-tail));
        }
      }
    }
  }
  destroy(){this.pause();this.disposeGL?.();this.gl=null;this.ctx=null;}
  alignFlow(){const t=(performance.now()-this.epoch)/1000;this.flowOffset=t*.105+Math.sin(t*.64)*.012;}
  capturePhase(){const t=(performance.now()-this.epoch)/1000;this.returnPhase=-t*.105-Math.sin(t*.64)*.012+this.flowOffset;}
  pause(){cancelAnimationFrame(this.frame);this.canvas.dataset.motion='stopped';}
  start(signal: AbortSignal,still: ()=>boolean=()=>false) {
    cancelAnimationFrame(this.frame);
    const step=(now: number)=>{if(signal.aborted)return;const stationary=still();this.canvas.dataset.motion=stationary?'still':'flowing';this.render(now,stationary);if(!stationary)this.frame=requestAnimationFrame(step);};
    if(this.boundSignal!==signal){this.boundSignal=signal;signal.addEventListener('abort',()=>{cancelAnimationFrame(this.frame);this.alpha=0;this.progress=-1;this.closing=-1;this.forming=-1;this.returning=-1;this.canvas.dataset.motion='stopped';this.render(performance.now());},{once:true});}
    this.frame=requestAnimationFrame(step);
  }
}
