using System;

namespace Comprehensive.Models
{
    public class Person
    {
        public string Name { get; }
        public int Age { get; }

        public Person(string name, int age)
        {
            Name = name;
            Age = age;
        }

        public virtual string Greet()
        {
            return $"Hi, I'm {Name}";
        }

        public override string ToString()
        {
            return $"Person(Name={Name}, Age={Age})";
        }
    }
}
