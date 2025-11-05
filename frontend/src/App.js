import React, { useState } from 'react';
import './App.css';
import QueryForm from './components/QueryForm';
import QueryResults from './components/QueryResults';
import GradientText from './components/GradientText';
import Particles from './components/Particles';

function App() {
  const [queryResult, setQueryResult] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);

  const handleQuery = async (queryData) => {
    setLoading(true);
    setError(null);
    setQueryResult(null);

    try {
      const response = await fetch('/query', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(queryData),
      });

      if (!response.ok) {
        const errorData = await response.text();
        throw new Error(errorData || 'Query failed');
      }

      const data = await response.json();
      setQueryResult(data);
    } catch (err) {
      setError(err.message || 'Failed to execute query');
      console.error('Query error:', err);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="App">
      <div className="particles-background">
        <Particles
          particleCount={200}
          particleSpread={10}
          speed={0.1}
          particleColors={['#ffffff']}
          moveParticlesOnHover={true}
          particleHoverFactor={2}
          alphaParticles={false}
          particleBaseSize={100}
          sizeRandomness={1}
          cameraDistance={20}
          disableRotation={false}
        />
      </div>
      <GradientText>
        <h1>Minitrue</h1>
      </GradientText>
      <p className="shiny-text">Decentralized Time-Series Database Query Interface</p>

      <QueryForm onSubmit={handleQuery} loading={loading} />
      
      {error && (
        <div className="error-message">
          <strong>Error:</strong> {error}
        </div>
      )}

      {queryResult && <QueryResults result={queryResult} />}

    </div>
  );
}

export default App;

