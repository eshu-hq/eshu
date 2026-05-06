<?php
class Person {
    public $name;
    public $age;
    
    function __construct($name, $age) {
        $this->name = $name;
        $this->age = $age;
    }
    
    function introduce() {
        return "Hi, I'm {$this->name} and I'm {$this->age} years old";
    }
}

$person = new Person("Alex", 22);
echo $shashank->introduce() . "\n";
