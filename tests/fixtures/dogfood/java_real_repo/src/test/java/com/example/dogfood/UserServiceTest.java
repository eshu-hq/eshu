// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// JUnit-shaped test file mirroring the src/test/java layout convention
// used by Spring Boot-style services. Synthetic content.

package com.example.dogfood;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

import org.junit.jupiter.api.Test;

public class UserServiceTest {
    @Test
    public void createReturnsPersistedUser() {
        UserService service = new UserService();
        User created = service.create("Dogfood", "dogfood@example.invalid");
        assertEquals("Dogfood", service.getOrThrow(created.getId()).getName());
    }

    @Test
    public void getOrThrowRaisesWhenMissing() {
        UserService service = new UserService();
        assertThrows(UserNotFoundException.class, () -> service.getOrThrow("missing"));
    }
}
