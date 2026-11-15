import { useId } from 'react';

type Tone = 'red' | 'purple' | 'blue' | 'green';
const tones: Record<Tone, string> = { red:'#FF4053', purple:'#965EEB', blue:'#00AAFF', green:'#02D15C' };

export function DecorativeFlower({ tone='purple', className='' }: {tone?:Tone;className?:string}) {
  const id=useId().replace(/:/g,'');
  const petal=`${id}-petal`, center=`${id}-center`, shadow=`${id}-shadow`;
  return <svg aria-hidden="true" viewBox="0 0 300 300" className={`decor-flower ${className}`} xmlns="http://www.w3.org/2000/svg">
    <defs>
      <radialGradient id={petal} cx="27%" cy="20%" r="84%"><stop offset="0" stopColor="#FFFFFF"/><stop offset=".17" stopColor={tones[tone]}/><stop offset=".68" stopColor={tones[tone]}/><stop offset="1" stopColor="#171717"/></radialGradient>
      <radialGradient id={center} cx="31%" cy="23%" r="80%"><stop offset="0" stopColor="#FFFFFF"/><stop offset=".24" stopColor={tones[tone]}/><stop offset=".72" stopColor={tones[tone]}/><stop offset="1" stopColor="#171717"/></radialGradient>
      <filter id={shadow} x="-55%" y="-55%" width="210%" height="210%"><feDropShadow dx="2" dy="13" stdDeviation="10" floodColor="#171717" floodOpacity=".26"/></filter>
    </defs>
    <g filter={`url(#${shadow})`}>{Array.from({length:8},(_,index)=><g key={index} transform={`rotate(${index*45} 150 150)`}><ellipse cx="150" cy="78" rx="42" ry="70" fill={`url(#${petal})`} stroke="#FFFFFF" strokeOpacity=".45" strokeWidth="1.3"/><ellipse cx="136" cy="59" rx="11" ry="30" fill="#FFFFFF" opacity=".17" transform="rotate(17 136 59)"/></g>)}<circle cx="150" cy="150" r="43" fill={`url(#${center})`} stroke="#FFFFFF" strokeOpacity=".6" strokeWidth="2"/><ellipse cx="136" cy="130" rx="17" ry="9" fill="#FFFFFF" opacity=".48" transform="rotate(-24 136 130)"/></g>
  </svg>;
}
