// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// Spring @Service-shaped class informed by public patterns in Spring
// Boot-style services. Synthetic content, hand-authored for this fixture.

package com.example.dogfood;

import java.util.HashMap;
import java.util.Map;
import java.util.Optional;
import org.springframework.stereotype.Service;

@Service
public class UserService {
    private final Map<String, User> users = new HashMap<>();

    public Optional<User> findById(String id) {
        return Optional.ofNullable(users.get(id));
    }

    public User create(String name, String email) {
        User user = new User(generateId(), name, email);
        users.put(user.getId(), user);
        return user;
    }

    public User getOrThrow(String id) {
        return findById(id).orElseThrow(() -> new UserNotFoundException(id));
    }

    private String generateId() {
        return java.util.UUID.randomUUID().toString();
    }
}
