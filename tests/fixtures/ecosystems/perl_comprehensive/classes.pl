#!/usr/bin/perl
use strict;
use warnings;

package Animal;

sub new {
    my ($class, %args) = @_;
    return bless {
        name    => $args{name} || 'Unknown',
        species => $args{species} || 'Unknown',
    }, $class;
}

sub name { return $_[0]->{name} }
sub species { return $_[0]->{species} }

sub speak {
    my ($self) = @_;
    return $self->{name} . " makes a sound";
}

package Dog;
use parent -norequire, 'Animal';

sub new {
    my ($class, %args) = @_;
    $args{species} = 'Canine';
    return $class->SUPER::new(%args);
}

sub speak {
    my ($self) = @_;
    return $self->{name} . " barks";
}

sub fetch {
    my ($self, $item) = @_;
    return $self->{name} . " fetches $item";
}

package Cat;
use parent -norequire, 'Animal';

sub new {
    my ($class, %args) = @_;
    $args{species} = 'Feline';
    return $class->SUPER::new(%args);
}

sub speak {
    my ($self) = @_;
    return $self->{name} . " meows";
}

1;
