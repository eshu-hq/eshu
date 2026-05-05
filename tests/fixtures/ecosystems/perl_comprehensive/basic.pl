#!/usr/bin/perl
use strict;
use warnings;

package Comprehensive;

use Exporter qw(import);
our @EXPORT_OK = qw(greet add process_items);

sub greet {
    my ($name) = @_;
    return "Hello, $name!";
}

sub add {
    my ($a, $b) = @_;
    return $a + $b;
}

sub process_items {
    my ($items_ref, $transform) = @_;
    return [map { $transform->($_) } @$items_ref];
}

1;
