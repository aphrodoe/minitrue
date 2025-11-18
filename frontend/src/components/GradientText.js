import React from 'react';
import './GradientText.css';

export default function GradientText({
  children,
  className = '',
  colors = ['#40ffaa', '#4079ff', '#40ffaa', '#4079ff', '#40ffaa'],
  animationSpeed = 8,
  showBorder = false
}) {
  const gradientStyle = {
    backgroundImage: `linear-gradient(to right, ${colors.join(', ')})`,
    backgroundSize: '300% 100%',
    backgroundClip: 'text',
    WebkitBackgroundClip: 'text',
    color: 'transparent',
    '--animation-duration': `${animationSpeed}s`
  };

  const containerStyle = {
    '--animation-duration': `${animationSpeed}s`
  };

  return (
    <div className={`animated-gradient-text ${className}`} style={containerStyle}>
      {showBorder && <div className="gradient-overlay" style={gradientStyle}></div>}
      <div className="text-content">
        {React.cloneElement(children, { 
          style: { 
            backgroundImage: `linear-gradient(to right, ${colors.join(', ')})`,
            backgroundSize: '300% 100%',
            backgroundClip: 'text',
            WebkitBackgroundClip: 'text',
            color: 'transparent',
            ...(children.props?.style || {})
          },
          className: `gradient-text-element ${children.props?.className || ''}`
        })}
      </div>
    </div>
  );
}

