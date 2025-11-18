import React, { useState } from 'react';
import './App.css';
import QueryForm from './components/QueryForm';
import QueryResults from './components/QueryResults';
import RealTimeMonitor from './components/RealTimeMonitor';

function App() {
  const [activeTab, setActiveTab] = useState('query');
  return (
    <div className="App">
      <button onClick={() => setActiveTab('query')}>Query Data</button>
      <button onClick={() => setActiveTab('realtime')}>Real-Time Monitor</button>
      {activeTab === 'query' ? <QueryForm onSubmit={() => {}} loading={false} /> : <RealTimeMonitor />}
      <QueryResults result={null} />
    </div>
  );
}

export default App;
