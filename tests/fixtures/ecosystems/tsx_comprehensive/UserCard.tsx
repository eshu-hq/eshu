import React, { memo, useCallback } from 'react';
import { Button } from './Button';

interface User {
  id: string;
  name: string;
  email: string;
}

interface UserCardProps {
  user: User;
  onDelete: (id: string) => void;
}

export const UserCard: React.FC<UserCardProps> = memo(({ user, onDelete }) => {
  const handleDelete = useCallback(() => {
    onDelete(user.id);
  }, [user.id, onDelete]);

  return (
    <div className="user-card">
      <h3>{user.name}</h3>
      <p>{user.email}</p>
      <Button variant="danger" onClick={handleDelete}>Delete</Button>
    </div>
  );
});

UserCard.displayName = 'UserCard';
