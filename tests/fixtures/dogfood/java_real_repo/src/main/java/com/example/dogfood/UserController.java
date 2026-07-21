// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// Spring MVC @RestController route shape informed by public patterns in
// Spring Boot-style services. Synthetic content, hand-authored for this
// fixture.

package com.example.dogfood;

import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
public class UserController {
    private final UserService userService;

    public UserController(UserService userService) {
        this.userService = userService;
    }

    @GetMapping("/users/{id}")
    public User show(@PathVariable String id) {
        return userService.getOrThrow(id);
    }

    @PostMapping("/users")
    public User create(@RequestParam String name, @RequestParam String email) {
        return userService.create(name, email);
    }

    @GetMapping("/health")
    public String healthCheck() {
        return "ok";
    }
}
