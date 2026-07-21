// Dogfood real-repo fixture for eshu-hq/eshu#5399. Synthetic content.

package com.example.dogfood;

public class UserNotFoundException extends RuntimeException {
    public UserNotFoundException(String userId) {
        super("user not found: " + userId);
    }
}
