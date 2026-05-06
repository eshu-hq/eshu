import React, { useState, useEffect } from 'react';
import { Button } from './Button';
import { UserCard } from './UserCard';

interface AppProps {
  title: string;
  debug?: boolean;
}

interface User {
  id: string;
  name: string;
  email: string;
}

const App: React.FC<AppProps> = ({ title, debug = false }) => {
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetchUsers().then(setUsers).finally(() => setLoading(false));
  }, []);

  async function fetchUsers(): Promise<User[]> {
    const response = await fetch('/api/users');
    return response.json();
  }

  const handleDelete = (id: string) => {
    setUsers(prev => prev.filter(u => u.id !== id));
  };

  if (loading) return <div>Loading...</div>;

  return (
    <div className="app">
      <h1>{title}</h1>
      {debug && <pre>{JSON.stringify(users, null, 2)}</pre>}
      {users.map(user => (
        <UserCard key={user.id} user={user} onDelete={handleDelete} />
      ))}
      <Button onClick={() => fetchUsers().then(setUsers)}>Refresh</Button>
    </div>
  );
};

export default App;
