/// Class patterns.

abstract class Shape {
  double get area;
  double get perimeter;

  String describe() => '${runtimeType}: area=$area';
}

class Circle extends Shape {
  final double radius;

  Circle(this.radius);

  @override
  double get area => 3.14159 * radius * radius;

  @override
  double get perimeter => 2 * 3.14159 * radius;
}

class Rectangle extends Shape {
  final double width;
  final double height;

  Rectangle(this.width, this.height);

  @override
  double get area => width * height;

  @override
  double get perimeter => 2 * (width + height);
}

class Person {
  final String name;
  final int age;

  Person(this.name, this.age);

  String greet() => 'Hi, I\'m $name';

  @override
  String toString() => 'Person($name, $age)';
}

class Employee extends Person {
  final String department;

  Employee(String name, int age, this.department) : super(name, age);

  @override
  String greet() => 'Hi, I\'m $name from $department';
}
