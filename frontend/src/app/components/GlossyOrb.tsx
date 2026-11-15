export function GlossyOrb({tone='blue',className=''}:{tone?:'blue'|'red'|'purple'|'green';className?:string}) {
  return <div aria-hidden="true" className={`glossy-orb orb-${tone} ${className}`}><span className="orb-reflection" /></div>;
}
