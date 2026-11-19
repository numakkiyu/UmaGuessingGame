precision highp float;
uniform vec2 size;uniform vec2 resolution;uniform float time;uniform float amount;uniform float progress;uniform float closing;uniform float forming;uniform float returning;uniform float returnPhase;uniform float flowOffset;uniform float failure;
float sm(float x){x=clamp(x,0.,1.);return x*x*x*(x*(x*6.-15.)+10.);}
float pulse(float x,float center,float sharpness){return pow(.5+.5*cos(6.2831853*(x-center)),sharpness);}
float around(float start,float target,float p){float delta=mod(target-start+.5,1.)-.5;return start+delta*p;}
vec3 palette(float x){return .57+.43*cos(6.2831853*(x+vec3(0.,.33,.67)));}
void main(){
 vec2 p=vec2(gl_FragCoord.x,resolution.y-gl_FragCoord.y)/resolution*size,halfSize=size*.5;
 float perimeter=2.*(size.x+size.y);
 float depth=min(min(p.x,size.x-p.x),min(p.y,size.y-p.y));
 // Radial projection keeps the perimeter coordinate continuous across corner bisectors.
 vec2 q=p-halfSize,n=q/halfSize;
 vec2 rim=halfSize+q/max(.0001,max(abs(n.x),abs(n.y)));
 float s;
 if(abs(n.y)>=abs(n.x)){
   s=n.y<0.?(halfSize.x-rim.x)/perimeter:(halfSize.x+size.y+rim.x)/perimeter;
 }else{
   s=n.x<0.?(halfSize.x+rim.y)/perimeter:(halfSize.x+size.y+size.x+size.y-rim.y)/perimeter;
 }
 float phase=-time*.105-sin(time*.64)*.012+flowOffset;
 float head=phase,head2=phase+.5,concentrate=0.,edgeAmount=amount;
 float white=0.,seamFeed=0.,bloom=1.,frontAge=1.;
 if(forming>=0.){
   float travel=sm(forming/.84),colorIn=sm((forming-.84)/.16);
   float fromTop=mod(-s+1.,1.),fromBottom=mod(.5-s+1.,1.);
   float distanceAlong=min(fromTop,fromBottom);
   float visited=sm((travel-distanceAlong)*perimeter/9.);
   frontAge=travel-distanceAlong;
   bloom=sm((forming-.18)/.62)*sm(frontAge/.20);
   head=-travel;head2=.5-travel;
   edgeAmount=mix(visited,1.,colorIn);
   white=1.-colorIn;
   float distance=perimeter*travel,t=min(1.,distance/size.y);
   float tail=size.y*(t-pow(t,6.)+3.*pow(t,5.)-2.5*pow(t,4.));
   float consumed=sm((tail-halfSize.y*.92)/(halfSize.y*.08));
   seamFeed=(1.-smoothstep(.45,1.1,abs(p.x-halfSize.x)))*(1.-sm((tail-abs(p.y-halfSize.y))/7.))*(1.-consumed);
 }
 if(returning>=0.){
   float collect=sm(returning/.46),middle=(halfSize.x+halfSize.y)/perimeter;
   head=around(returnPhase,middle,collect);head2=around(returnPhase+.5,middle+.5,collect);
   concentrate=collect;edgeAmount=1.-sm((returning-.38)/.44);
 }
 float energy=pulse(s,head,mix(8.,28.,concentrate))+.90*pulse(s,head2,mix(11.,30.,concentrate));
 float width=8.+min(size.x,size.y)*(.034+.05*energy);
 float inward=exp(-pow(depth/max(1.,width*.68),1.28));
 float core=1.-smoothstep(.65,1.45,depth);
 // The narrow white trace precedes the bloom; its wake softens as it ages.
 float alpha=mix(core,inward,bloom)*(.97+.03*min(energy,1.))*edgeAmount;
 vec3 color=mix(palette(s*2.+time*.085),vec3(1.),white);
 color=mix(color,vec3(1.,.12,.09),failure*.9);
 if(seamFeed>alpha){alpha=seamFeed;color=vec3(1.);}
 if(closing>=0.){
   float travel=sm(closing);
   float left=halfSize.x*travel,right=size.x-halfSize.x*travel;
   float l=1.-smoothstep(.35,1.05,abs(p.x-left));
   float r=1.-smoothstep(.35,1.05,abs(p.x-right));
   alpha=max(l,r);color=vec3(1.);
 }
 if(returning>=0.){
   float reach=sm((returning-.34)/.56),left=halfSize.x*reach,right=size.x-left;
   float tip=1.-sm((min(p.x-left,right-p.x))/8.);
   float rise=sm((returning-.30)/.14),settle=sm((returning-.64)/.36);
   float dy=abs(p.y-halfSize.y),line=1.-smoothstep(.45,1.15,dy);
   float haze=exp(-pow(dy/mix(17.,1.,settle),1.5))*(1.-settle);
   float beam=max(line,haze*.86)*tip*rise;
   float handoff=sm((returning-.46)/.48);
   alpha=mix(alpha,max(alpha*(1.-handoff),beam),handoff);
   color=mix(color,vec3(1.),handoff);
 }
 if(progress>=0.){
   float nx=abs(p.x-halfSize.x)/halfSize.x,dist=nx*nx*(3.-2.*nx);
   float local=sm(((progress-.16)/.84-.06*dist)/.94),gap=(halfSize.y+4.)*local;
   float breath=sin(3.14159265*local)*sin(3.14159265*dist)*min(5.,size.y*.008);
   float dd=abs(p.y-halfSize.y)-gap-(p.y<halfSize.y?breath:-breath*.45);
   float extent=halfSize.x+20.;
   float radiate=sm(progress/.16),fade=1.-sm((progress-.78)/.22);
   float core=1.-smoothstep(.45,1.15,abs(dd));
   float halo=exp(-pow(abs(dd)/mix(1.,18.,radiate),1.5))*radiate*.88;
   float film=max(core,halo)*(1.-sm((abs(p.x-halfSize.x)-extent+20.)/20.));
   alpha=film*fade;color=vec3(1.);
 }
 gl_FragColor=vec4(color,clamp(alpha,0.,1.));
}